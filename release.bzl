load("@rules_go//go:def.bzl", "go_cross_binary")

def release_binaries():
    for name in ["git-review", "git-repo-server", "git-review-hub"]:
        for platform_name in ["linux_amd64", "linux_arm64"]:
            go_cross_binary(
                name = name + "-" + platform_name,
                target = "//cmd/" + name,
                platform = "@rules_go//go/toolchain:" + platform_name,
            )
