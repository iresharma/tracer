package parser

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathMeta is the namespace/pod/uid/container derived purely from a
// kubelet container log file's path — no Kubernetes API access needed.
type PathMeta struct {
	Namespace string
	Pod       string
	PodUID    string
	Container string
}

// ParsePodLogPath extracts metadata from a path shaped like
// "/var/log/pods/<namespace>_<pod>_<uid>/<container>/<n>.log". Kubernetes
// namespace and pod names are DNS-1123 (no underscores allowed), so
// splitting the pod-directory name on "_" unambiguously yields exactly
// [namespace, pod, uid].
func ParsePodLogPath(path string) (PathMeta, error) {
	container := filepath.Base(filepath.Dir(path))
	podDir := filepath.Base(filepath.Dir(filepath.Dir(path)))

	parts := strings.Split(podDir, "_")
	if len(parts) != 3 {
		return PathMeta{}, fmt.Errorf("unrecognized pod log directory name %q (path %q)", podDir, path)
	}
	if container == "" || container == "." || container == "/" {
		return PathMeta{}, fmt.Errorf("could not determine container from path %q", path)
	}

	return PathMeta{
		Namespace: parts[0],
		Pod:       parts[1],
		PodUID:    parts[2],
		Container: container,
	}, nil
}
