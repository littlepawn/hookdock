package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxOutput = 256 * 1024

// whitelistEnv 从进程环境里只挑任务运行必需的变量，避免把服务端凭证带给任务进程
func whitelistEnv(environ []string) []string {
	allow := map[string]bool{
		"PATH": true, "HOME": true, "TZ": true, "LANG": true, "LC_ALL": true,
		"SHELL": true, "TMPDIR": true,
	}
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i > 0 && allow[kv[:i]] {
			out = append(out, kv)
		}
	}
	return out
}

type Result struct {
	ExitCode  int64
	Output    string
	Truncated bool
	Err       string
	TimedOut  bool
}

// 有界输出缓冲
type capBuf struct {
	mu  sync.Mutex
	b   bytes.Buffer
	max int
	tr  bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if c.b.Len()+n > c.max {
		room := c.max - c.b.Len()
		if room > 0 {
			c.b.Write(p[:room])
		}
		c.tr = true
		return n, nil
	}
	c.b.Write(p)
	return n, nil
}

func (c *capBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

// Run 执行 argv，超时杀整个进程组（TERM → 5s → KILL）
func Run(ctx context.Context, argv []string, env map[string]string, timeoutSec int64) Result {
	if len(argv) == 0 {
		return Result{ExitCode: -1, Err: "exec_argv 为空"}
	}
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// 环境变量白名单：任务进程不继承容器全部环境（防止读出 HOOKDOCK_ADMIN_TOKEN 等凭证），
	// 只给运行必需项 + 任务上下文专用变量（HOOKDOCK_JOB_*）
	cmd.Env = whitelistEnv(os.Environ())
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	buf := &capBuf{max: maxOutput}
	cmd.Stdout = buf
	cmd.Stderr = buf
	cmd.Stdin = nil

	// 超时后杀进程组
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid := cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			time.Sleep(5 * time.Second)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}()
		return nil
	}
	cmd.WaitDelay = 8 * time.Second

	err := cmd.Run()
	res := Result{Output: buf.String(), Truncated: buf.tr}
	if cmd.ProcessState != nil {
		res.ExitCode = int64(cmd.ProcessState.ExitCode())
	} else {
		res.ExitCode = -1
	}
	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Err = fmt.Sprintf("执行超时（%ds）", timeoutSec)
		res.ExitCode = -1
		return res
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Err = fmt.Sprintf("退出码 %d", ee.ExitCode())
		} else {
			res.Err = err.Error()
		}
	}
	return res
}

// ---------- 模板替换 ----------

var tplRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.\-\[\]]+)\s*\}\}`)

// Expand 把 argv 里的 {{ request.body.foo }} / {{ request.query.x }} / {{ run.attempt }} 替换掉
func Expand(argv []string, vars map[string]any) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = tplRe.ReplaceAllStringFunc(a, func(m string) string {
			key := strings.TrimSpace(tplRe.FindStringSubmatch(m)[1])
			if v, ok := lookup(vars, key); ok {
				return v
			}
			return ""
		})
	}
	return out
}

func lookup(root map[string]any, path string) (string, bool) {
	cur := any(root)
	for _, seg := range strings.Split(path, ".") {
		if cur == nil {
			return "", false
		}
		switch node := cur.(type) {
		case map[string]any:
			cur = node[seg]
		case []any:
			idx, err := strconv.Atoi(strings.Trim(seg, "[]"))
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}
