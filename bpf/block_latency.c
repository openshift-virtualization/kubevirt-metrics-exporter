// SPDX-License-Identifier: GPL-2.0
//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "bits.bpf.h"
#include "common.h"

char LICENSE[] SEC("license") = "GPL";

struct req_key {
	__u32 dev;
	__u64 sector;
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_BLOCK_ENTRIES);
	__type(key, struct req_key);
	__type(value, __u64);
} block_start SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_HIST_ENTRIES);
	__type(key, struct block_hist_key);
	__type(value, struct hist);
} block_hists SEC(".maps");

static struct hist zero_hist;

static __always_inline __u8 classify_op(const char *rwbs)
{
	switch (rwbs[0]) {
	case 'R': return 0;
	case 'W': return 1;
	case 'D': return 2;
	case 'F': return 3;
	default:  return 0xff;
	}
}

SEC("tracepoint/block/block_rq_issue")
int tracepoint__block__block_rq_issue(struct trace_event_raw_block_rq *ctx)
{
	struct req_key rk = {};
	__u64 ts;

	if (classify_op(ctx->rwbs) == 0xff)
		return 0;

	rk.dev = ctx->dev;
	rk.sector = ctx->sector;
	ts = bpf_ktime_get_ns();

	bpf_map_update_elem(&block_start, &rk, &ts, BPF_ANY);
	return 0;
}

SEC("tracepoint/block/block_rq_complete")
int tracepoint__block__block_rq_complete(struct trace_event_raw_block_rq_completion *ctx)
{
	struct req_key rk = {};
	struct block_hist_key hk = {};
	struct hist *histp;
	__u64 *tsp, slot;
	__s64 delta;

	rk.dev = ctx->dev;
	rk.sector = ctx->sector;

	tsp = bpf_map_lookup_elem(&block_start, &rk);
	if (!tsp)
		return 0;

	delta = (__s64)(bpf_ktime_get_ns() - *tsp);
	bpf_map_delete_elem(&block_start, &rk);

	if (delta < 0)
		return 0;

	delta /= 1000U;
	if (delta == 0)
		delta = 1;

	hk.dev = ctx->dev;
	hk.op = classify_op(ctx->rwbs);
	if (hk.op == 0xff)
		return 0;

	histp = bpf_map_lookup_elem(&block_hists, &hk);
	if (!histp) {
		bpf_map_update_elem(&block_hists, &hk, &zero_hist, BPF_ANY);
		histp = bpf_map_lookup_elem(&block_hists, &hk);
		if (!histp)
			return 0;
	}

	slot = log2l(delta);
	if (slot >= MAX_SLOTS)
		slot = MAX_SLOTS - 1;

	__sync_fetch_and_add(&histp->slots[slot], 1);
	return 0;
}
