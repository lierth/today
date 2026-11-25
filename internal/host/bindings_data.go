package host

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"today/internal/boardlink"
)

// errNoHost is what every binding answers when the panel was built without a
// running host, which only happens while the TypeScript client is generated.
var errNoHost = errors.New("今天没有在运行时没有这个功能")

// This file is the settings window's half of the Panel service. It stays a
// thin adapter too: every rule lives in server, the only work here is picking
// the fields the window shows and naming them for TypeScript.

// Tag is one chip under the quick-add field: what the parser found, and the
// exact slice of the draft the × removes.
type Tag struct {
	// Kind is "date", "time" or "repeat".
	Kind string `json:"kind"`
	// Label is what the chip shows: "11月20日", "10:30", "每周一三五".
	Label string `json:"label"`
	// Text is the part of the draft the chip stands for.
	Text string `json:"text"`
}

// DeviceRow is one board in the settings window.
type DeviceRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Online   bool   `json:"online"`
	Battery  string `json:"battery"`
	Climate  string `json:"climate"`
	Since    string `json:"since"`
	Firmware string `json:"firmware"`
	City     string `json:"city"`
}

// BackupRow is one file in the data tab.
type BackupRow struct {
	Name  string `json:"name"`
	Size  string `json:"size"`
	When  string `json:"when"`
	Where string `json:"where"`
}

// UpdateView is what the about tab shows about updates.
type UpdateView struct {
	Version  string `json:"version"`
	Status   string `json:"status"`
	Latest   string `json:"latest"`
	URL      string `json:"url"`
	Updating bool   `json:"updating"`
}

// SettingsView is the whole settings payload. One call because the window
// opens as a unit; a round trip per card would pop the cards in one by one.
type SettingsView struct {
	Version      string            `json:"version"`
	Dev          bool              `json:"dev"`
	Settings     map[string]string `json:"settings"`
	Devices      []DeviceRow       `json:"devices"`
	Backups      []BackupRow       `json:"backups"`
	BackupError  string            `json:"backup_error"`
	LastLocal    string            `json:"last_local"`
	LastCloud    string            `json:"last_cloud"`
	CloudOn      bool              `json:"cloud_on"`
	Update       UpdateView        `json:"update"`
	Firmware     string            `json:"firmware"`
	Zones        []string          `json:"zones"`
	Funnel       string            `json:"funnel"`
	FunnelDetail string            `json:"funnel_detail"`
	FunnelAction string            `json:"funnel_action"`
	LocalNet     string            `json:"local_net"`
	ProxySummary string            `json:"proxy_summary"`
	DataRoot     string            `json:"data_root"`
	DataNote     string            `json:"data_note"`
	// SourceURL is the project page, built from the repository the release
	// build was stamped with. Empty for a source build, which then shows no
	// link rather than the bare github.com it used to open.
	SourceURL string `json:"source_url"`
}

// SettingsData is the settings window's one read.
func (p *Panel) SettingsData(ctx context.Context) (SettingsView, error) {
	if p == nil || p.h == nil || p.h.srv == nil {
		return SettingsView{}, errNoHost
	}
	srv := p.h.srv
	settings, err := srv.SettingsSnapshot(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	devices, err := srv.Service().Devices(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	rows := make([]DeviceRow, 0, len(devices))
	for _, d := range devices {
		rows = append(rows, DeviceRow{
			ID: d.ID, Name: d.Name, Online: d.Online,
			Battery: d.BatteryLabel, Climate: d.Climate, Since: d.Since,
			Firmware: d.Firmware, City: d.City,
		})
	}
	status := srv.BackupStatus()
	snaps, snapErr := srv.SnapshotList()
	backups := make([]BackupRow, 0, len(snaps))
	for _, s := range snaps {
		where := "本地"
		if s.Cloud {
			where = "iCloud"
		}
		backups = append(backups, BackupRow{
			Name: s.Name, Size: byteSize(s.Size),
			When: s.ModTime.Format("2006-01-02 15:04"), Where: where,
		})
	}
	backupErr := status.Error
	if snapErr != nil {
		backupErr = snapErr.Error()
	}
	fw := srv.FirmwareVersion(ctx)
	p.h.mu.Lock()
	funnel, detail, action := p.h.funnel.State, p.h.funnel.Detail, p.h.funnel.ActionURL
	localNet := string(p.h.localNet)
	updating := p.h.updating
	latest, updateURL := p.h.latest, p.h.updateURL
	p.h.mu.Unlock()
	return SettingsView{
		Version: Version, Dev: p.h.dev, Settings: settings,
		Devices: rows, Backups: backups, BackupError: backupErr,
		LastLocal: status.LastLocal, LastCloud: status.LastCloud, CloudOn: status.CloudOn,
		Update: UpdateView{
			Version: Version, Status: p.h.updateStatus(), Latest: latest,
			URL: updateURL, Updating: updating,
		},
		Firmware: fw,
		Zones:    srv.ZoneChoices(ctx),
		Funnel:   string(funnel), FunnelDetail: detail, FunnelAction: action,
		LocalNet:     localNet,
		ProxySummary: srv.ProxySummary(ctx),
		DataRoot:     p.h.srv.Store().Path(), DataNote: p.h.srv.BindNote(),
		SourceURL: sourceURL(),
	}, nil
}

// sourceURL is the GitHub page of the build's own repository.
func sourceURL() string {
	if ReleaseRepo == "" {
		return ""
	}
	return "https://github.com/" + ReleaseRepo
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

// SetSetting writes one key through the server, so the tray menu rebuilds and
// the hooks (login item, funnel, local network) run exactly as from the page.
func (p *Panel) SetSetting(ctx context.Context, key, value string) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	if err := p.h.srv.SetSetting(ctx, key, value); err != nil {
		return err
	}
	p.h.kick()
	return nil
}

// DiscoverBoards finds displays on the Mac's directly attached LAN.
func (p *Panel) DiscoverBoards(ctx context.Context) ([]boardlink.Board, error) {
	if p == nil || p.h == nil || p.h.srv == nil {
		return nil, errNoHost
	}
	return p.h.srv.DiscoverBoards(ctx)
}

// PairBoard binds the selected display using its on-screen code.
func (p *Panel) PairBoard(ctx context.Context, id, code, name string) (DeviceRow, error) {
	if p == nil || p.h == nil || p.h.srv == nil {
		return DeviceRow{}, errNoHost
	}
	device, err := p.h.srv.PairBoard(ctx, id, code, name)
	if err != nil {
		return DeviceRow{}, err
	}
	p.h.kick()
	return DeviceRow{ID: device.ID, Name: device.Name, Since: "等待连接"}, nil
}

// AddDevice creates a board with the id and name typed in the window.
func (p *Panel) AddDevice(ctx context.Context, id, name string) (DeviceRow, error) {
	if p == nil || p.h == nil {
		return DeviceRow{}, errNoHost
	}
	d, err := p.h.srv.AddDevice(ctx, id, name)
	if err != nil {
		return DeviceRow{}, err
	}
	return DeviceRow{ID: d.ID, Name: d.Name, Since: "从未上线"}, nil
}

// RevokeDevice cuts a board off; its token stops working on the next frame.
func (p *Panel) RevokeDevice(ctx context.Context, id string) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	if err := p.h.srv.RevokeDevice(ctx, id); err != nil {
		return err
	}
	p.h.kick()
	return nil
}

// DeviceToken mints a token and returns the four lines to type on the board.
// The token is shown once: only its hash is stored.
func (p *Panel) DeviceToken(ctx context.Context, id string) (SetupLines, error) {
	if p == nil || p.h == nil {
		return SetupLines{}, errNoHost
	}
	res, err := p.h.srv.GenerateDeviceToken(ctx, id)
	if err != nil {
		return SetupLines{}, err
	}
	return SetupLines{Token: res.Token, Hint: res.Hint, Lines: res.Commands}, nil
}

// SetupLines is a minted device token with its typing instructions.
type SetupLines struct {
	Token string   `json:"token"`
	Hint  string   `json:"hint"`
	Lines []string `json:"lines"`
}

// PreviewFrame renders the frame a board would draw now, at 1×–4×, as a PNG
// data URL the window can put straight in an <img>.
func (p *Panel) PreviewFrame(ctx context.Context, deviceID string, scale int) (string, error) {
	if p == nil || p.h == nil {
		return "", errNoHost
	}
	png, err := p.h.srv.PreviewFramePNG(ctx, deviceID, scale)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// BackupNow writes one snapshot now, for the button in the data tab.
func (p *Panel) BackupNow(ctx context.Context) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	day := time.Now()
	if loc, err := p.h.srv.Service().Location(ctx); err == nil {
		day = day.In(loc)
	}
	return p.h.srv.RunBackup(day.Format("2006-01-02"))
}

// RestoreBackup replaces the database with the named snapshot. The window asks
// first; this is the call behind "restore".
func (p *Panel) RestoreBackup(ctx context.Context, name string) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	if err := p.h.srv.RestoreSnapshot(name); err != nil {
		return err
	}
	p.h.kick()
	return nil
}

// CheckUpdate asks the update feed; the answer lands in the about tab and in
// the notification the menu already sent.
func (p *Panel) CheckUpdate(ctx context.Context) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	p.h.checkUpdate()
	return nil
}

// DownloadUpdate starts the full update: check, fetch the asset, verify it and
// stage it next to the running app. It returns as soon as the work is queued;
// the window re-reads SettingsData to follow the progress line.
func (p *Panel) DownloadUpdate() {
	if p == nil || p.h == nil {
		return
	}
	p.h.beginUpdate()
}

// UploadFirmware reads a .bin the window picked and stages it for the boards.
func (p *Panel) UploadFirmware(ctx context.Context, path, version string) (string, error) {
	if p == nil || p.h == nil {
		return "", errNoHost
	}
	return p.h.srv.UploadFirmware(ctx, path, version)
}

// PushFirmware sends the staged build to the boards on their next frame.
func (p *Panel) PushFirmware(ctx context.Context) (string, error) {
	if p == nil || p.h == nil {
		return "", errNoHost
	}
	return p.h.srv.PushFirmware(ctx)
}

// ChooseFirmware opens the file picker for a .bin and returns its path. The
// dialog lives on the Go side because only the app can open a native panel.
func (p *Panel) ChooseFirmware(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title: "选择固件文件",
		Filters: []mygo.FileFilter{
			{Name: "固件镜像", Extensions: []string{"bin"}},
		},
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

// ConfirmRestore asks first, then restores: the dialog is native so it stays in
// front of the window even when the window is not the key window.
func (p *Panel) ConfirmRestore(ctx context.Context, name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("先选一个备份")
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:               mygo.MessageWarning,
		Title:              "恢复备份",
		Message:            "用 " + name + " 覆盖现在的事项？",
		Detail:             "恢复后本机登录会失效，需要重新登录；设备会自动取到新的一帧。恢复前的数据会另存一份，出问题可以再恢复回来。",
		Buttons:            []string{"取消", "恢复"},
		DefaultButton:      0,
		CancelButton:       0,
		DestructiveButtons: []int{1},
	})
	if err != nil {
		return false, err
	}
	if res.Button != 1 {
		return false, nil
	}
	if err := p.RestoreBackup(ctx, name); err != nil {
		return false, err
	}
	return true, nil
}

// LocalNetworkHelp opens the system pane where "Local Network" lives, the same
// place the menu's warning item goes.
func (p *Panel) LocalNetworkHelp() error { return mygo.Shell.OpenExternal(localNetworkURL) }

func (p *Panel) DeleteBackup(ctx context.Context, name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("先选择一个备份")
	}
	result, err := mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageWarning, Title: "删除备份", Message: "删除 " + name + "？", Detail: "这份备份文件将永久删除，当前事项不受影响。", Buttons: []string{"取消", "删除"}, DefaultButton: 0, CancelButton: 0, DestructiveButtons: []int{1}})
	if err != nil || result.Button != 1 {
		return false, err
	}
	if err := p.h.srv.DeleteSnapshot(name); err != nil {
		return false, err
	}
	return true, nil
}

// OpenURL opens an address in the default browser (release notes, Tailscale's
// own setup pages).
func (p *Panel) OpenURL(ctx context.Context, url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("只能打开 http(s) 地址")
	}
	return mygo.Shell.OpenExternal(url)
}

// CopyPassword puts the login password on the clipboard and clears it again
// after a minute, like the menu item does.
func (p *Panel) CopyPassword() { p.h.CopyPassword() }

// CopyText is for the smaller copy buttons (LAN address, setup lines).
func (p *Panel) CopyText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	mygo.Clipboard.WriteText(text)
	return nil
}

// PendingUpdateNote is the release the updater staged, if any. Empty when the
// app is not holding a downloaded build.
func (p *Panel) PendingUpdateNote() string {
	if p == nil || p.h == nil {
		return ""
	}
	p.h.mu.Lock()
	defer p.h.mu.Unlock()
	return p.h.updater.Ready
}
