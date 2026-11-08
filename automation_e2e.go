//go:build e2e

package main

// 开发时用 MYGO_AUTOMATION=/tmp/bookdesk.sock GOFLAGS=-tags=e2e 启动，
// 就能从外部脚本驱动窗口、执行页面脚本，做端到端验证。
import _ "github.com/egoist/mygo/plugins/automation"
