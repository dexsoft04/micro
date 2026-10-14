package golang

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/build"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testMainGo   = "package main; import \"fmt\"; func main() { fmt.Println(\"HelloWorld\") }"
	testSecondGo = "package main; import \"fmt\"; func init() { fmt.Println(\"Init\") }"
)

func TestGolangBuilder(t *testing.T) {
	t.Run("NoArchive", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte(testMainGo))
		err := testBuilder(t, buf)
		assert.Nil(t, err, "No error should be returned")
	})

	t.Run("InvalidArchive", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte(testMainGo))
		err := testBuilder(t, buf, build.Archive("foo"))
		assert.Error(t, err, "An error should be returned")
	})

	t.Run("TarArchive", func(t *testing.T) {
		// Create a tar writer
		tf := bytes.NewBuffer(nil)
		tw := tar.NewWriter(tf)

		// Add some files to the archive.
		var files = []struct {
			Name, Body string
		}{
			{"main.go", testMainGo},
			{"second.go", testSecondGo},
		}
		for _, file := range files {
			hdr := &tar.Header{
				Name: file.Name,
				Mode: 0600,
				Size: int64(len(file.Body)),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(file.Body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}

		err := testBuilder(t, tf, build.Archive("tar"))
		assert.Nil(t, err, "No error should be returned")
	})

	t.Run("ZipArchive", func(t *testing.T) {
		// Create a buffer to write our archive to.
		buf := new(bytes.Buffer)

		// Create a new zip archive.
		w := zip.NewWriter(buf)
		defer w.Close()

		// Add some files to the archive.
		var files = []struct {
			Name, Body string
		}{
			{"main.go", testMainGo},
			{"second.go", testSecondGo},
		}
		for _, file := range files {
			f, err := w.Create(file.Name)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.Write([]byte(file.Body))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		err := testBuilder(t, buf, build.Archive("zip"))
		assert.Nil(t, err, "No error should be returned")
	})
}

func TestBuildEnvUsesSecondaryPublicProxyBeforeDirect(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_PARAMETERS='url.git@github.com:.insteadOf=https://github.com'",
	}
	env := buildEnv(base)

	assert.Equal(t, base, env[:len(base)])
	assert.Contains(t, env, "GIT_CONFIG_PARAMETERS='url.git@github.com:.insteadOf=https://github.com' 'url.https://github.com/.insteadOf=https://github.com/' 'url.https://github.com/dexsoft04/micro.insteadOf=https://github.com/dexsoft04/micro'")
	assert.Contains(t, env, "GO111MODULE=auto")
	assert.Contains(t, env, "GOPROXY=https://goproxy.cn|https://goproxy.io|direct")
	assert.Contains(t, env, "GOPRIVATE=gitee.com")
}

func TestBuildEnvGitConfigIsReadable(t *testing.T) {
	cmd := exec.Command("git", "config", "--get", "url.https://github.com/.insteadOf")
	cmd.Env = buildEnv([]string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	})
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, string(out))
	assert.Equal(t, "https://github.com/\n", string(out))
}

func TestBuildEnvPreservesGitHubHTTPSAgainstGlobalRewrite(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".gitconfig")
	config := "[url \"git@github.com:\"]\n\tinsteadOf = https://github.com\n" +
		"[http]\n\tproxy = http://127.0.0.1:1\n"
	assert.NoError(t, os.WriteFile(configPath, []byte(config), 0600))

	cmd := exec.Command("git", "ls-remote", "https://github.com/example/repository")
	cmd.Env = buildEnv([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Dir(configPath),
		"GIT_CONFIG_GLOBAL=" + configPath,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TRACE=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=false",
	})
	out, err := cmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(out), "remote-https")
	assert.NotContains(t, string(out), "git@github.com:")
}

func TestBuildEnvPreservesMicroHTTPSAgainstEqualLengthRewrite(t *testing.T) {
	for _, prefix := range []string{"https://github.com", "https://github.com/"} {
		for _, source := range []string{"global", "parameters", "count"} {
			t.Run(prefix+"/"+source, func(t *testing.T) {
				configPath := filepath.Join(t.TempDir(), ".gitconfig")
				config := "[url \"git@gitee.com:\"]\n insteadOf = https://gitee.com/\n"
				env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(configPath), "GIT_CONFIG_GLOBAL=" + configPath, "GIT_CONFIG_NOSYSTEM=1"}
				switch source {
				case "global":
					config += "[url \"git@github.com:\"]\n insteadOf = " + prefix + "\n"
				case "parameters":
					env = append(env, "GIT_CONFIG_PARAMETERS='url.git@github.com:.insteadOf="+prefix+"'")
				case "count":
					env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url.git@github.com:.insteadOf", "GIT_CONFIG_VALUE_0="+prefix)
					probe := exec.Command("git", "config", "--get", "url.git@github.com:.insteadOf")
					probe.Env = env
					out, err := probe.CombinedOutput()
					if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(out) == 0 {
						t.Skip("Git does not support GIT_CONFIG_COUNT")
					}
					require.NoError(t, err, string(out))
					require.Equal(t, prefix, strings.TrimSpace(string(out)))
				}
				require.NoError(t, os.WriteFile(configPath, []byte(config), 0600))
				for _, url := range []string{"https://github.com/dexsoft04/micro", "https://github.com/dexsoft04/micro.git", "https://gitee.com/team/private"} {
					cmd := exec.Command("git", "ls-remote", "--get-url", url)
					cmd.Env = buildEnv(env)
					out, err := cmd.CombinedOutput()
					require.NoError(t, err, string(out))
					want := url
					if strings.HasPrefix(url, "https://gitee.com/") {
						want = "git@gitee.com:team/private"
					}
					require.Equal(t, want, strings.TrimSpace(string(out)))
				}
			})
		}
	}
}

func TestBuildEnvProxyFailuresReachDirect(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	goPath, err := locateGo()
	require.NoError(t, err)
	repo := t.TempDir()
	gitConfig := filepath.Join(t.TempDir(), ".gitconfig")
	require.NoError(t, os.WriteFile(gitConfig, []byte("[url \"file://"+filepath.ToSlash(repo)+"\"]\n insteadOf = https://github.com/proxytest/module\n"), 0600))
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(gitConfig),
		"GIT_CONFIG_GLOBAL=" + gitConfig, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_ALLOW_PROTOCOL=file", "GIT_TERMINAL_PROMPT=0",
		"GOSUMDB=off", "GONOPROXY=", "GONOSUMDB=", "GOTOOLCHAIN=local", "GOWORK=off",
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	runGit("init", "-q", repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/proxytest/module\n\ngo 1.20\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "module.go"), []byte("package module\n"), 0600))
	runGit("-C", repo, "add", ".")
	runGit("-C", repo, "-c", "user.name=Proxy Test", "-c", "user.email=proxy@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Initial module")
	runGit("-C", repo, "tag", "v1.0.0")

	for _, statuses := range [][2]int{{404, 500}, {500, 500}, {404, 404}, {410, 410}} {
		t.Run(fmt.Sprintf("%d/%d", statuses[0], statuses[1]), func(t *testing.T) {
			var firstRequests, secondRequests atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstRequests.Add(1)
				w.WriteHeader(statuses[0])
			}))
			defer first.Close()
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondRequests.Add(1)
				w.WriteHeader(statuses[1])
			}))
			defer second.Close()
			cmdEnv := buildEnv(env)
			proxy, ok := envValue(cmdEnv, "GOPROXY")
			require.True(t, ok)
			proxy = strings.ReplaceAll(proxy, "https://goproxy.cn", first.URL)
			proxy = strings.ReplaceAll(proxy, "https://goproxy.io", second.URL)
			cmdEnv = append(cmdEnv, "GOPROXY="+proxy, "GOMODCACHE="+t.TempDir(), "GOCACHE="+t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, goPath, "mod", "download", "-modcacherw", "github.com/proxytest/module@v1.0.0")
			cmd.Dir = t.TempDir()
			cmd.Env = cmdEnv
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Positive(t, firstRequests.Load())
			require.Positive(t, secondRequests.Load())
		})
	}
}

func testBuilder(t *testing.T, buf io.Reader, opts ...build.Option) error {
	// setup the build
	build, err := NewBuilder()
	if err != nil {
		return fmt.Errorf("Error creating the build: %v", err)
	}

	// build the source
	res, err := build.Build(buf, opts...)
	if err != nil {
		return fmt.Errorf("Error building source: %v", err)
	}

	// write the binary to a tmp file and make it executable
	file, err := ioutil.TempFile(os.TempDir(), "res")
	if err != nil {
		return fmt.Errorf("Error creating tmp output file: %v", err)
	}
	if _, err := io.Copy(file, res); err != nil {
		return fmt.Errorf("Error copying binary to tmp file: %v", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(file.Name(), 0111); err != nil {
		return err
	}
	defer os.Remove(file.Name())

	// execute the binary
	cmd := exec.Command(file.Name())
	outp, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Error executing binary: %v", err)
	}
	if !strings.Contains(string(outp), "HelloWorld") {
		return fmt.Errorf("Output does not contain HelloWorld")
	}
	// when an archive is used we also check for the second file to be loaded
	if len(opts) > 0 && !strings.Contains(string(outp), "Init") {
		return fmt.Errorf("Output does not contain Init")
	}

	return nil
}
