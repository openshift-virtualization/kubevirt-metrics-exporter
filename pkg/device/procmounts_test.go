package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMountInfoLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    MountEntry
		wantErr bool
	}{
		{
			name: "ext4 mount",
			line: "36 35 8:1 / /mnt/data rw,noatime shared:1 - ext4 /dev/sda1 rw,errors=continue",
			want: MountEntry{
				MountID:    36,
				ParentID:   35,
				Major:      8,
				Minor:      1,
				Root:       "/",
				MountPoint: "/mnt/data",
				FSType:     "ext4",
				Source:     "/dev/sda1",
			},
		},
		{
			name: "NFS mount",
			line: "100 50 0:45 / /var/lib/kubelet/pods/abc/volumes/nfs-vol/mount rw,relatime shared:10 - nfs 10.0.0.1:/export rw",
			want: MountEntry{
				MountID:    100,
				ParentID:   50,
				Major:      0,
				Minor:      45,
				Root:       "/",
				MountPoint: "/var/lib/kubelet/pods/abc/volumes/nfs-vol/mount",
				FSType:     "nfs",
				Source:     "10.0.0.1:/export",
			},
		},
		{
			name: "device mapper",
			line: "42 35 253:3 / /var/lib/containers rw,relatime shared:2 - xfs /dev/mapper/rhel-containers rw,seclabel",
			want: MountEntry{
				MountID:    42,
				ParentID:   35,
				Major:      253,
				Minor:      3,
				Root:       "/",
				MountPoint: "/var/lib/containers",
				FSType:     "xfs",
				Source:     "/dev/mapper/rhel-containers",
			},
		},
		{
			name:    "too few fields",
			line:    "36 35 8:1",
			wantErr: true,
		},
		{
			name:    "missing separator",
			line:    "36 35 8:1 / /mnt rw ext4 /dev/sda1 rw",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMountInfoLine(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMountInfoLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.MountID != tt.want.MountID || got.ParentID != tt.want.ParentID {
				t.Errorf("IDs: got %d/%d, want %d/%d", got.MountID, got.ParentID, tt.want.MountID, tt.want.ParentID)
			}
			if got.Major != tt.want.Major || got.Minor != tt.want.Minor {
				t.Errorf("dev: got %d:%d, want %d:%d", got.Major, got.Minor, tt.want.Major, tt.want.Minor)
			}
			if got.MountPoint != tt.want.MountPoint {
				t.Errorf("mount point: got %q, want %q", got.MountPoint, tt.want.MountPoint)
			}
			if got.FSType != tt.want.FSType {
				t.Errorf("fs type: got %q, want %q", got.FSType, tt.want.FSType)
			}
			if got.Source != tt.want.Source {
				t.Errorf("source: got %q, want %q", got.Source, tt.want.Source)
			}
		})
	}
}

func TestParseMountInfoFile(t *testing.T) {
	content := `22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw,errors=continue
36 22 253:0 / /home rw,noatime shared:2 - xfs /dev/mapper/vg0-home rw
100 22 0:45 / /mnt/nfs rw - nfs 10.0.0.1:/share rw
`
	dir := t.TempDir()
	path := filepath.Join(dir, "mountinfo")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseMountInfo(path)
	if err != nil {
		t.Fatalf("ParseMountInfo() error = %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	if entries[0].FSType != "ext4" {
		t.Errorf("entry 0 FSType = %q, want ext4", entries[0].FSType)
	}
	if entries[1].FSType != "xfs" {
		t.Errorf("entry 1 FSType = %q, want xfs", entries[1].FSType)
	}
	if entries[2].FSType != "nfs" {
		t.Errorf("entry 2 FSType = %q, want nfs", entries[2].FSType)
	}
}
