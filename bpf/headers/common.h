#ifndef __COMMON_H
#define __COMMON_H

#define MAX_SLOTS 26
#define MAX_BLOCK_ENTRIES 10240
#define MAX_NFS_ENTRIES 10240
#define MAX_NFS_KPROBE_ENTRIES 10240
#define MAX_HIST_ENTRIES 1024

struct block_hist_key {
	__u32 dev;
	__u8 op;
	__u8 pad[3];
};

struct nfs_hist_key {
	__u32 dev;
	__u8 op;
	__u8 pad[3];
};

struct hist {
	__u64 slots[MAX_SLOTS];
};

#endif /* __COMMON_H */
