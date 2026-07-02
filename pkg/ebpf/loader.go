package ebpf

import (
	"fmt"
	"log/slog"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type Programs struct {
	blockObjs     *blockObjects
	nfsObjs       *nfsObjects
	nfsKprobeObjs *nfsKprobeObjects
	links         []link.Link

	BlockHists      *ebpf.Map
	NfsHists        *ebpf.Map
	NfsKprobeHists  *ebpf.Map
	BlockActive     bool
	NFSActive       bool
	NFSKprobeActive bool
}

func LoadAndAttach(enableBlock, enableNFS, enableNFSKprobe bool, blockMapSize, nfsMapSize, nfsKprobeMapSize int, log *slog.Logger) (*Programs, error) {
	p := &Programs{}

	if enableBlock {
		if err := p.loadBlock(blockMapSize, log); err != nil {
			log.Warn("block tracepoints unavailable — block monitoring disabled", "error", err)
		} else {
			p.BlockActive = true
		}
	}

	if enableNFS {
		if err := p.loadNFS(nfsMapSize, log); err != nil {
			log.Warn("NFS tracepoints unavailable — NFS monitoring disabled", "error", err)
		} else {
			p.NFSActive = true
		}
	}

	if enableNFSKprobe {
		if err := p.loadNFSKprobe(nfsKprobeMapSize, log); err != nil {
			log.Warn("NFS kprobes unavailable — NFS VFS monitoring disabled", "error", err)
		} else {
			p.NFSKprobeActive = true
		}
	}

	requested := enableBlock || enableNFS || enableNFSKprobe
	active := p.BlockActive || p.NFSActive || p.NFSKprobeActive
	if requested && !active {
		return nil, fmt.Errorf("all requested eBPF subsystems failed to load")
	}

	return p, nil
}

func (p *Programs) loadBlock(mapSize int, log *slog.Logger) error {
	spec, err := loadBlock()
	if err != nil {
		return fmt.Errorf("loading block spec: %w", err)
	}

	if mapSize > 0 {
		spec.Maps["block_start"].MaxEntries = uint32(mapSize)
	}

	objs := &blockObjects{}
	if err := spec.LoadAndAssign(objs, nil); err != nil {
		return fmt.Errorf("loading block objects: %w", err)
	}
	p.blockObjs = objs
	p.BlockHists = objs.BlockHists

	l, err := link.Tracepoint("block", "block_rq_issue", objs.TracepointBlockBlockRqIssue, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching block_rq_issue: %w", err)
	}
	p.links = append(p.links, l)
	log.Info("attached tracepoint", "group", "block", "name", "block_rq_issue")

	l, err = link.Tracepoint("block", "block_rq_complete", objs.TracepointBlockBlockRqComplete, nil)
	if err != nil {
		p.Close()
		return fmt.Errorf("attaching block_rq_complete: %w", err)
	}
	p.links = append(p.links, l)
	log.Info("attached tracepoint", "group", "block", "name", "block_rq_complete")

	return nil
}

func (p *Programs) loadNFS(mapSize int, log *slog.Logger) error {
	spec, err := loadNfs()
	if err != nil {
		return fmt.Errorf("loading NFS spec: %w", err)
	}

	if mapSize > 0 {
		spec.Maps["nfs_start"].MaxEntries = uint32(mapSize)
	}

	objs := &nfsObjects{}
	if err := spec.LoadAndAssign(objs, nil); err != nil {
		return fmt.Errorf("loading NFS objects: %w", err)
	}

	nfsTracepoints := []struct {
		name string
		prog *ebpf.Program
	}{
		{"nfs_initiate_read", objs.RawTpNfsInitiateRead},
		{"nfs_initiate_write", objs.RawTpNfsInitiateWrite},
		{"nfs_readpage_done", objs.RawTpNfsReadpageDone},
		{"nfs_writeback_done", objs.RawTpNfsWritebackDone},
	}

	var nfsLinks []link.Link
	for _, tp := range nfsTracepoints {
		l, err := link.AttachRawTracepoint(link.RawTracepointOptions{
			Name:    tp.name,
			Program: tp.prog,
		})
		if err != nil {
			for _, nl := range nfsLinks {
				nl.Close()
			}
			objs.Close()
			return fmt.Errorf("attaching raw_tracepoint/%s: %w", tp.name, err)
		}
		nfsLinks = append(nfsLinks, l)
		log.Info("attached raw tracepoint", "name", tp.name)
	}

	p.nfsObjs = objs
	p.NfsHists = objs.NfsHists
	p.links = append(p.links, nfsLinks...)
	return nil
}

func (p *Programs) loadNFSKprobe(mapSize int, log *slog.Logger) error {
	spec, err := loadNfsKprobe()
	if err != nil {
		return fmt.Errorf("loading NFS kprobe spec: %w", err)
	}

	if mapSize > 0 {
		spec.Maps["nfs_kprobe_start"].MaxEntries = uint32(mapSize)
	}

	objs := &nfsKprobeObjects{}
	if err := spec.LoadAndAssign(objs, nil); err != nil {
		return fmt.Errorf("loading NFS kprobe objects: %w", err)
	}

	type kprobeTarget struct {
		name     string
		prog     *ebpf.Program
		optional bool
	}

	kprobes := []kprobeTarget{
		{"nfs_file_read", objs.KprobeNfsFileRead, false},
		{"nfs_file_write", objs.KprobeNfsFileWrite, false},
		{"nfs_file_open", objs.KprobeNfsFileOpen, false},
		{"nfs4_file_open", objs.KprobeNfs4FileOpen, true},
		{"nfs_getattr", objs.KprobeNfsGetattr, false},
	}

	var kprobeLinks []link.Link
	var attachedFns []string
	for _, kp := range kprobes {
		l, err := link.Kprobe(kp.name, kp.prog, nil)
		if err != nil {
			if kp.optional {
				log.Debug("optional kprobe unavailable", "function", kp.name, "error", err)
				continue
			}
			for _, kl := range kprobeLinks {
				kl.Close()
			}
			objs.Close()
			return fmt.Errorf("attaching kprobe/%s: %w", kp.name, err)
		}
		kprobeLinks = append(kprobeLinks, l)
		attachedFns = append(attachedFns, kp.name)
		log.Info("attached kprobe", "function", kp.name)
	}

	for _, fn := range attachedFns {
		l, err := link.Kretprobe(fn, objs.KretprobeNfsVfs, nil)
		if err != nil {
			for _, kl := range kprobeLinks {
				kl.Close()
			}
			objs.Close()
			return fmt.Errorf("attaching kretprobe/%s: %w", fn, err)
		}
		kprobeLinks = append(kprobeLinks, l)
		log.Info("attached kretprobe", "function", fn)
	}

	p.nfsKprobeObjs = objs
	p.NfsKprobeHists = objs.NfsKprobeHists
	p.links = append(p.links, kprobeLinks...)
	return nil
}

func (p *Programs) Close() {
	for _, l := range p.links {
		l.Close()
	}
	if p.blockObjs != nil {
		p.blockObjs.Close()
	}
	if p.nfsObjs != nil {
		p.nfsObjs.Close()
	}
	if p.nfsKprobeObjs != nil {
		p.nfsKprobeObjs.Close()
	}
}
