package cmd

import (
	"fmt"
	"runtime"

	ver "github.com/hashicorp/go-version"
	"github.com/urfave/cli/v2"
)

var (
	// populated by ldflags
	GitCommit string
	GitTag    string
	BuildDate string

	version    = "v3.12.3"
	prerelease = "mcbeam-v3.12.3" // blank if full release
)

func init() {
	cli.VersionPrinter = func(ctx *cli.Context) {
		fmt.Fprintf(ctx.App.Writer, "%s version %s %s/%s\n", ctx.App.Name, ctx.App.Version, runtime.GOOS, runtime.GOARCH)
	}
}

func buildVersion() string {
	verStr := version
	if prerelease != "" {
		verStr = fmt.Sprintf("%s-%s", version, prerelease)
	}

	// check for git tag via ldflags
	if len(GitTag) > 0 {
		verStr = GitTag
	}

	// make sure we fail fast (panic) if bad version - this will get caught in CI tests
	ver.Must(ver.NewVersion(verStr))
	return verStr
}
