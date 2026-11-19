package release

import (
	"errors"
	"fmt"
	"time"
)

// State is the update machine. The names are the ones the menu and the
// settings page use, so a log line and a label always agree.
type State string

const (
	StateIdle        State = "idle"
	StateChecking    State = "checking"
	StateAvailable   State = "available"
	StateDownloading State = "downloading"
	StateVerifying   State = "verifying"
	StateReady       State = "ready"
	StateInstalling  State = "installing"
	StateFailed      State = "failed"
)

// EventKind is one input to the machine.
type EventKind string

const (
	EventCheck      EventKind = "check"
	EventAvailable  EventKind = "available"
	EventNone       EventKind = "none"     // check finished, nothing newer
	EventDownload   EventKind = "download" // start (or resume) the download
	EventProgress   EventKind = "progress" // still downloading
	EventDownloaded EventKind = "downloaded"
	EventVerified   EventKind = "verified"
	EventReady      EventKind = "ready" // automatic mode finished early
	EventInstall    EventKind = "install"
	EventFail       EventKind = "fail"
	EventReset      EventKind = "reset" // dismiss / skip / retry
)

// Event carries the transition input. Only the fields that matter for the
// kind are read, so callers can leave the rest zero.
type Event struct {
	Kind     EventKind
	Version  string
	Reason   string
	Progress int // 0..100
}

// Transition is the machine's answer: the next state plus a human sentence the
// caller may show.
type Transition struct {
	State State
	Note  string
}

// Next applies one event. It is the whole update state machine: every state
// and every failure path is reachable through it, which is why it lives here
// with table-driven tests instead of inside the tray code.
//
// The rules:
//
//   - A check can start from idle, available, ready or failed; from checking,
//     downloading, verifying or installing it is ignored (the work in flight
//     would be wasted).
//   - "download" is legal from available and from failed (a retry).
//   - "verified" only from verifying; "install" only from ready.
//   - Any failure lands in failed with the reason kept.
func Next(cur State, ev Event) Transition {
	if cur == "" {
		cur = StateIdle
	}
	fail := func(reason string) Transition {
		if reason == "" {
			reason = "未知原因"
		}
		return Transition{State: StateFailed, Note: reason}
	}
	switch ev.Kind {
	case EventCheck:
		switch cur {
		case StateIdle, StateAvailable, StateReady, StateFailed:
			return Transition{State: StateChecking, Note: "正在检查更新…"}
		}
		return Transition{State: cur, Note: "已有一次检查在进行"}
	case EventAvailable:
		if cur != StateChecking {
			return Transition{State: cur, Note: "不在检查状态，忽略"}
		}
		if ev.Version == "" {
			return fail("更新源没有给出版本号")
		}
		return Transition{State: StateAvailable, Note: "发现新版本 " + ev.Version}
	case EventNone:
		if cur != StateChecking {
			return Transition{State: cur, Note: "不在检查状态，忽略"}
		}
		return Transition{State: StateIdle, Note: "已是最新版本"}
	case EventDownload:
		if cur != StateAvailable && cur != StateFailed {
			return Transition{State: cur, Note: "当前不能下载"}
		}
		return Transition{State: StateDownloading, Note: "正在下载…"}
	case EventProgress:
		if cur != StateDownloading {
			return Transition{State: cur, Note: ""}
		}
		return Transition{State: StateDownloading, Note: progressNote(ev.Progress)}
	case EventDownloaded:
		if cur != StateDownloading {
			return Transition{State: cur, Note: "没有下载中的文件"}
		}
		return Transition{State: StateVerifying, Note: "正在校验签名…"}
	case EventVerified:
		if cur != StateVerifying {
			return Transition{State: cur, Note: "不在校验状态"}
		}
		return Transition{State: StateReady, Note: "更新已就绪"}
	case EventReady:
		// The automatic download shortcut: available → downloading →
		// verifying → ready with no user click.
		if cur == StateAvailable || cur == StateDownloading || cur == StateVerifying {
			return Transition{State: StateReady, Note: "更新已就绪"}
		}
		return Transition{State: cur, Note: ""}
	case EventInstall:
		if cur != StateReady {
			return Transition{State: cur, Note: "没有已就绪的更新"}
		}
		return Transition{State: StateInstalling, Note: "正在替换并重启…"}
	case EventFail:
		return fail(ev.Reason)
	case EventReset:
		switch cur {
		case StateChecking:
			return Transition{State: cur, Note: "检查还在进行"}
		case StateInstalling:
			return Transition{State: cur, Note: "正在安装"}
		case StateDownloading:
			return Transition{State: StateFailed, Note: "下载已取消"}
		}
		return Transition{State: StateIdle, Note: ""}
	}
	return Transition{State: cur, Note: "未知事件"}
}

func progressNote(p int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return fmt.Sprintf("正在下载 … %d%%", p)
}

// ErrNotReady is what the installer returns when asked to install without a
// verified download.
var ErrNotReady = errors.New("release: no verified update to install")

// Timing constants the host uses. They are here so the cadence is one place.
const (
	FirstCheckAfter = 5 * time.Minute
	CheckInterval   = 6 * time.Hour
	// InstallWait bounds how long the replacement script waits for the old
	// process to exit.
	InstallWait = 30 * time.Second
	// IdleFor is how long the person must be away before a background
	// download counts as "idle time".
	IdleFor = 10 * time.Minute
)
