const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const mygo_mod = b.createModule(.{
        .root_source_file = b.path("../mygo/zig/sdk/mygo.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
    });

    const app_mod = b.createModule(.{
        .root_source_file = b.path("zig-src/app.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
        .strip = optimize != .debug,
        .imports = &.{
            .{ .name = "mygo", .module = mygo_mod },
        },
    });

    const lib = b.addLibrary(.{
        .name = "bookdesk",
        .linkage = .dynamic,
        .root_module = app_mod,
    });

    // Install into resources/ so `mygo generate`, `mygo dev`, and `mygo build` find it.
    const install_lib = b.addInstallArtifact(lib, .{
        .dest_dir = .{ .override = .{ .custom = "../resources" } },
    });
    b.getInstallStep().dependOn(&install_lib.step);
}
