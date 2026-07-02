// SPDX-License-Identifier: GPL-2.0
//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "bits.bpf.h"
#include "common.h"

char LICENSE[] SEC("license") = "GPL";

struct nfs_kprobe_start_val {
	__u64 ts;
	__u32 dev;
	__u8 op;
	__u8 pad[3];
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_NFS_KPROBE_ENTRIES);
	__type(key, __u64);
	__type(value, struct nfs_kprobe_start_val);
} nfs_kprobe_start SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_HIST_ENTRIES);
	__type(key, struct nfs_hist_key);
	__type(value, struct hist);
} nfs_kprobe_hists SEC(".maps");

static struct hist zero_hist;

static __always_inline void record_start(__u32 dev, __u8 op)
{
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	struct nfs_kprobe_start_val start = {
		.ts = bpf_ktime_get_ns(),
		.dev = dev,
		.op = op,
	};
	bpf_map_update_elem(&nfs_kprobe_start, &pid_tgid, &start, BPF_ANY);
}

SEC("kprobe/nfs_file_read")
int BPF_KPROBE(kprobe_nfs_file_read, struct kiocb *iocb)
{
	record_start(BPF_CORE_READ(iocb, ki_filp, f_inode, i_sb, s_dev), 0);
	return 0;
}

SEC("kprobe/nfs_file_write")
int BPF_KPROBE(kprobe_nfs_file_write, struct kiocb *iocb)
{
	record_start(BPF_CORE_READ(iocb, ki_filp, f_inode, i_sb, s_dev), 1);
	return 0;
}

SEC("kprobe/nfs_file_open")
int BPF_KPROBE(kprobe_nfs_file_open, struct inode *inode)
{
	record_start(BPF_CORE_READ(inode, i_sb, s_dev), 2);
	return 0;
}

SEC("kprobe/nfs4_file_open")
int BPF_KPROBE(kprobe_nfs4_file_open, struct inode *inode)
{
	record_start(BPF_CORE_READ(inode, i_sb, s_dev), 2);
	return 0;
}

/* nfs_getattr PARM1 is mnt_idmap* (6.3+) or user_namespace* (5.12+); path is always PARM2 */
SEC("kprobe/nfs_getattr")
int BPF_KPROBE(kprobe_nfs_getattr, void *first, struct path *path)
{
	record_start(BPF_CORE_READ(path, dentry, d_inode, i_sb, s_dev), 3);
	return 0;
}

SEC("kretprobe/nfs_file_read")
int BPF_KRETPROBE(kretprobe_nfs_vfs)
{
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	struct nfs_kprobe_start_val *start;
	struct nfs_hist_key hk = {};
	struct hist *histp;
	__s64 delta;
	__u64 slot;

	start = bpf_map_lookup_elem(&nfs_kprobe_start, &pid_tgid);
	if (!start)
		return 0;

	delta = (__s64)(bpf_ktime_get_ns() - start->ts);
	hk.dev = start->dev;
	hk.op = start->op;

	bpf_map_delete_elem(&nfs_kprobe_start, &pid_tgid);

	if (delta < 0)
		return 0;

	delta /= 1000U;
	if (delta == 0)
		delta = 1;

	histp = bpf_map_lookup_elem(&nfs_kprobe_hists, &hk);
	if (!histp) {
		bpf_map_update_elem(&nfs_kprobe_hists, &hk, &zero_hist, BPF_ANY);
		histp = bpf_map_lookup_elem(&nfs_kprobe_hists, &hk);
		if (!histp)
			return 0;
	}

	slot = log2l(delta);
	if (slot >= MAX_SLOTS)
		slot = MAX_SLOTS - 1;

	__sync_fetch_and_add(&histp->slots[slot], 1);
	return 0;
}
