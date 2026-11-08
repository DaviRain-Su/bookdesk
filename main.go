package main

import (
	"github.com/egoist/mygo/zig/host"
	_ "github.com/egoist/mygo/zig/host/automation"
	_ "github.com/egoist/mygo/zig/host/fetch"
	_ "github.com/egoist/mygo/zig/host/sqlite"
)

func main() {
	host.Main("bookdesk")
}
