// SPDX-License-Identifier: GPL-2.0
//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "bits.bpf.h"
#include "common.h"

char LICENSE[] SEC("license") = "GPL";

struct nfs_req_key {
	__u32 dev;
	__u32 count;
	__u64 offset;
	__u8 op;
	__u8 pad[7];
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_NFS_ENTRIES);
	__type(key, struct nfs_req_key);
	__type(value, __u64);
} nfs_start SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 256);
	__type(key, struct nfs_hist_key);
	__type(value, struct hist);
} nfs_hists SEC(".maps");

static struct hist zero_hist;

static __always_inline void nfs_fill_key(struct nfs_pgio_header *hdr,
					 struct nfs_req_key *nk, __u8 op)
{
	nk->dev = BPF_CORE_READ(hdr, inode, i_sb, s_dev);
	nk->offset = BPF_CORE_READ(hdr, args.offset);
	nk->count = BPF_CORE_READ(hdr, args.count);
	nk->op = op;
}

static __always_inline void nfs_record_latency(struct nfs_req_key *nk,
					       struct nfs_hist_key *hk)
{
	struct hist *histp;
	__u64 *tsp, slot;
	__s64 delta;

	tsp = bpf_map_lookup_elem(&nfs_start, nk);
	if (!tsp)
		return;

	delta = (__s64)(bpf_ktime_get_ns() - *tsp);
	bpf_map_delete_elem(&nfs_start, nk);

	if (delta < 0)
		return;

	delta /= 1000U;
	if (delta == 0)
		delta = 1;

	histp = bpf_map_lookup_elem(&nfs_hists, hk);
	if (!histp) {
		bpf_map_update_elem(&nfs_hists, hk, &zero_hist, BPF_ANY);
		histp = bpf_map_lookup_elem(&nfs_hists, hk);
		if (!histp)
			return;
	}

	slot = log2l(delta);
	if (slot >= MAX_SLOTS)
		slot = MAX_SLOTS - 1;

	__sync_fetch_and_add(&histp->slots[slot], 1);
}

SEC("raw_tracepoint/nfs_initiate_read")
int raw_tp_nfs_initiate_read(struct bpf_raw_tracepoint_args *ctx)
{
	struct nfs_req_key nk = {};
	__u64 ts = bpf_ktime_get_ns();

	nfs_fill_key((struct nfs_pgio_header *)ctx->args[0], &nk, 0);
	bpf_map_update_elem(&nfs_start, &nk, &ts, BPF_ANY);
	return 0;
}

SEC("raw_tracepoint/nfs_initiate_write")
int raw_tp_nfs_initiate_write(struct bpf_raw_tracepoint_args *ctx)
{
	struct nfs_req_key nk = {};
	__u64 ts = bpf_ktime_get_ns();

	nfs_fill_key((struct nfs_pgio_header *)ctx->args[0], &nk, 1);
	bpf_map_update_elem(&nfs_start, &nk, &ts, BPF_ANY);
	return 0;
}

/* Done TP_PROTO has rpc_task* as args[0], nfs_pgio_header* as args[1] */

SEC("raw_tracepoint/nfs_readpage_done")
int raw_tp_nfs_readpage_done(struct bpf_raw_tracepoint_args *ctx)
{
	struct nfs_req_key nk = {};
	struct nfs_hist_key hk = {};

	nfs_fill_key((struct nfs_pgio_header *)ctx->args[1], &nk, 0);
	hk.dev = nk.dev;
	hk.op = 0;

	nfs_record_latency(&nk, &hk);
	return 0;
}

SEC("raw_tracepoint/nfs_writeback_done")
int raw_tp_nfs_writeback_done(struct bpf_raw_tracepoint_args *ctx)
{
	struct nfs_req_key nk = {};
	struct nfs_hist_key hk = {};

	nfs_fill_key((struct nfs_pgio_header *)ctx->args[1], &nk, 1);
	hk.dev = nk.dev;
	hk.op = 1;

	nfs_record_latency(&nk, &hk);
	return 0;
}
