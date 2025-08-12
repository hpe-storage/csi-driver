package lock

import (
	"strings"
)

const (
	prefix = "/etcd-lock"
)

func addPrefix(key string) string {
	if !strings.HasPrefix(key, "/") {
		key = "/" + key
	}
	return prefix + key
}
