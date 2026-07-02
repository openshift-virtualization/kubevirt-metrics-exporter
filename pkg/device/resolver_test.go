package device

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestParseKubeletVolumePath(t *testing.T) {
	tests := []struct {
		name       string
		mountPoint string
		wantUID    string
		wantPVC    string
		wantOK     bool
	}{
		{
			name:       "NFS volume",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			wantUID:    "6a76960a-a927-4211-96e6-1f187b126e90",
			wantPVC:    "example",
			wantOK:     true,
		},
		{
			name:       "CSI volume with /mount suffix",
			mountPoint: "/var/lib/kubelet/pods/aabbccdd-1234-5678-9abc-def012345678/volumes/kubernetes.io~csi/pvc-deadbeef-0000-1111-2222-333344445555/mount",
			wantUID:    "aabbccdd-1234-5678-9abc-def012345678",
			wantPVC:    "pvc-deadbeef-0000-1111-2222-333344445555",
			wantOK:     true,
		},
		{
			name:       "local volume",
			mountPoint: "/var/lib/kubelet/pods/11111111-2222-3333-4444-555566667777/volumes/kubernetes.io~local-volume/local-pv-abc",
			wantUID:    "11111111-2222-3333-4444-555566667777",
			wantPVC:    "local-pv-abc",
			wantOK:     true,
		},
		{
			name:       "non-kubelet mount",
			mountPoint: "/mnt/data",
			wantOK:     false,
		},
		{
			name:       "root filesystem",
			mountPoint: "/",
			wantOK:     false,
		},
		{
			name:       "kubelet path without volume",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/containers/app",
			wantOK:     false,
		},
		{
			name:       "invalid UUID in path",
			mountPoint: "/var/lib/kubelet/pods/not-a-uuid/volumes/kubernetes.io~nfs/example",
			wantOK:     false,
		},
		{
			name:       "block device path does not match filesystem regex",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, pvc, ok := parseKubeletVolumePath(tt.mountPoint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if uid != tt.wantUID {
				t.Errorf("podUID = %q, want %q", uid, tt.wantUID)
			}
			if pvc != tt.wantPVC {
				t.Errorf("pvcName = %q, want %q", pvc, tt.wantPVC)
			}
		})
	}
}

func TestParseKubeletBlockDevicePath(t *testing.T) {
	tests := []struct {
		name       string
		mountPoint string
		wantUID    string
		wantPVC    string
		wantOK     bool
	}{
		{
			name:       "CSI block volume publish path",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantUID:    "c68a7381-7595-4de4-a594-cffe21c042fe",
			wantPVC:    "pvc-30f5aca9-100b-4759-b9d9-5d896081fd23",
			wantOK:     true,
		},
		{
			name:       "staging path (no pod UID)",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/staging/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/0001-0009-rook-ceph-0000000000000001-474bbf8f-2bec-4025-8391-5b316cdb801f",
			wantOK:     false,
		},
		{
			name:       "dev path (no pod UID at end)",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/dev/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantOK:     false,
		},
		{
			name:       "filesystem volume path does not match block regex",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, pvc, ok := parseKubeletBlockDevicePath(tt.mountPoint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if uid != tt.wantUID {
				t.Errorf("podUID = %q, want %q", uid, tt.wantUID)
			}
			if pvc != tt.wantPVC {
				t.Errorf("pvcName = %q, want %q", pvc, tt.wantPVC)
			}
		})
	}
}

func TestResolvePVCName(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		PVCByPVIndexName: PVCByPVIndexFunc,
	})

	indexer.Add(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-data-pvc", Namespace: "default"},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pvc-abcd-1234"},
	})

	r := &Resolver{pvcIndexer: indexer}

	tests := []struct {
		name   string
		pvName string
		want   string
	}{
		{"resolves PV to PVC name", "pvc-abcd-1234", "my-data-pvc"},
		{"returns PV name when not found", "pvc-unknown", "pvc-unknown"},
		{"returns empty string as-is", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.resolvePVCName(tt.pvName)
			if got != tt.want {
				t.Errorf("resolvePVCName(%q) = %q, want %q", tt.pvName, got, tt.want)
			}
		})
	}
}

func TestResolvePVCNameNilIndexer(t *testing.T) {
	r := &Resolver{}
	got := r.resolvePVCName("pvc-abcd-1234")
	if got != "pvc-abcd-1234" {
		t.Errorf("resolvePVCName with nil indexer = %q, want %q", got, "pvc-abcd-1234")
	}
}

func TestPodMetaMap(t *testing.T) {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	store.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-my-vm-abc",
			Namespace: "production",
			UID:       "uid-1234",
		},
	})
	store.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pod",
			Namespace: "default",
			UID:       "uid-5678",
		},
	})

	r := &Resolver{podStore: store}
	metas := r.podMetaMap()

	if len(metas) != 2 {
		t.Fatalf("len = %d, want 2", len(metas))
	}
	if metas["uid-1234"].Name != "virt-launcher-my-vm-abc" {
		t.Errorf("name = %q, want %q", metas["uid-1234"].Name, "virt-launcher-my-vm-abc")
	}
	if metas["uid-1234"].Namespace != "production" {
		t.Errorf("namespace = %q, want %q", metas["uid-1234"].Namespace, "production")
	}
	if metas["uid-5678"].Name != "other-pod" {
		t.Errorf("name = %q, want %q", metas["uid-5678"].Name, "other-pod")
	}
}

func TestPodMetaMapNilStore(t *testing.T) {
	r := &Resolver{}
	metas := r.podMetaMap()
	if len(metas) != 0 {
		t.Errorf("len = %d, want 0", len(metas))
	}
}
