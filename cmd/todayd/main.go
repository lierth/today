// Command todayd runs Today without the macOS shell: serve, render frames,
// decode captures, replay device ops and reset the password.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"today/internal/app"
	"today/internal/frame"
	"today/internal/server"
	"today/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "preview":
		err = runPreview(os.Args[2:])
	case "decode":
		err = runDecode(os.Args[2:])
	case "sim":
		err = runSim(os.Args[2:])
	case "passwd":
		err = runPasswd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "todayd:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `todayd — Today server and device tooling

  todayd serve   [--addr 8787] [--db path] [--dev]
  todayd preview [--db path] [--device dev1] [--out frame.png] [--png] [--scale 3] [--demo]
  todayd decode  <frame.bin>
  todayd sim     [--db path] <ops.jsonl>
  todayd passwd  [--db path]        (new password on stdin)
`)
}

func openStore(path string) (*store.Store, error) {
	if path == "" {
		path = app.DefaultDBPath()
	}
	return store.Open(path)
}

func location(ctx context.Context, st *store.Store) *time.Location {
	if tz, err := st.Setting(ctx, "timezone"); err == nil && tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (default: config.json, then "+app.DefaultAddr+")")
	db := fs.String("db", "", "database path")
	dev := fs.Bool("dev", false, "development mode: keep hands off Tailscale")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, pw, err := app.Boot(ctx, app.Config{Addr: *addr, DBPath: *db, Dev: *dev})
	if err != nil {
		return err
	}
	defer a.Close()
	if pw != "" {
		fmt.Printf("\n首次启动，已生成访问口令：%s\n打开 http://127.0.0.1%s/login 登录，随后可在设置页修改。\n\n",
			pw, portSuffix(a.Server.Addr()))
	}
	<-ctx.Done()
	return nil
}

func portSuffix(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ""
}

func runPreview(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	device := fs.String("device", "dev1", "device id")
	out := fs.String("out", "frame.png", "output file")
	pngOut := fs.Bool("png", false, "write a scaled PNG instead of the raw frame")
	scale := fs.Int("scale", 3, "PNG scale")
	demo := fs.Bool("demo", false, "render sample rows instead of the database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.EnsureDevice(ctx, *device, *device); err != nil {
		return err
	}
	loc := location(ctx, st)
	now := time.Now().In(loc)
	var items []store.Item
	if *demo {
		items = demoItems(now)
	} else {
		items, err = st.ItemsOn(ctx, now.Format(store.DateLayout))
		if err != nil {
			return err
		}
	}
	in := frame.Input{Now: now, Items: frameItems(items), CursorRow: -1, CursorStyle: frame.CursorStyleInvert}
	if *demo {
		in.CursorRow, in.CursorStyle = 1, frame.CursorStyleStrike
	}
	f, err := frame.Build(in)
	if err != nil {
		return err
	}
	file, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer file.Close()
	if *pngOut {
		if err := f.WritePNG(file, *scale); err != nil {
			return err
		}
	} else if _, err := file.Write(f.Encoded); err != nil {
		return err
	}
	fmt.Printf("%s: %dx%d rows=%d bitmap=%dB\n", *out, f.Width, f.Height, len(f.Rows), len(f.Bitmap))
	return nil
}

func frameItems(items []store.Item) []frame.Item {
	out := make([]frame.Item, 0, len(items))
	for _, it := range items {
		out = append(out, frame.Item{
			ID:       it.ID,
			Title:    it.Title,
			Done:     it.Done(),
			AllDay:   it.AllDay,
			Pinned:   it.Pinned,
			StartsAt: it.StartsAt,
		})
	}
	return out
}

func demoItems(now time.Time) []store.Item {
	at := func(h, m int) *time.Time {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
		return &t
	}
	return []store.Item{
		{ID: "1", Title: "晨会：同步本周排期", StartsAt: at(9, 30), AllDay: false, Status: store.StatusOpen},
		{ID: "2", Title: "写完墨水屏帧协议的文档和示例", StartsAt: at(10, 0), Status: store.StatusOpen},
		{ID: "3", Title: "给服务端补上 ETag / 304 的逻辑", StartsAt: at(14, 30), Status: store.StatusOpen},
		{ID: "4", Title: "一个特别特别长的标题，用来验证截断和省略号是否正常", Status: store.StatusDone, StartsAt: at(9, 0)},
		{ID: "5", Title: "晚饭后散步 30 分钟", StartsAt: at(19, 0), Status: store.StatusOpen},
	}
}

func runDecode(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: todayd decode <frame.bin>")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	f, err := frame.Decode(raw)
	if err != nil {
		return err
	}
	out := struct {
		Width    int      `json:"width"`
		Height   int      `json:"height"`
		Flags    uint8    `json:"flags"`
		Cursor   int      `json:"cursor_row"`
		Style    int      `json:"cursor_style"`
		Rows     int      `json:"row_count"`
		Footer   int      `json:"footer_count"`
		Bitmap   int      `json:"bitmap_bytes"`
		Checksum uint32   `json:"crc32"`
		RowIDs   []string `json:"row_ids"`
	}{
		Width: int(f.Width), Height: int(f.Height), Flags: f.Flags,
		Cursor: int(f.CursorRow), Style: int(f.CursorStyle),
		Rows: len(f.Rows), Footer: len(f.Footer),
		Bitmap: len(f.Bitmap), Checksum: f.EncodedChecksum,
	}
	for _, row := range f.Rows {
		out.RowIDs = append(out.RowIDs, shortID(row.UUID))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func shortID(id [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func runSim(args []string) error {
	fs := flag.NewFlagSet("sim", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	path := fs.String("ops", "", "ops file (positional also accepted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" && fs.NArg() == 1 {
		*path = fs.Arg(0)
	}
	if *path == "" {
		return fmt.Errorf("usage: todayd sim [--db path] <ops.jsonl>")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer f.Close()
	var applied, dup, failed int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var op struct {
			OpID   string `json:"op_id"`
			ItemID string `json:"item_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "skip %q: %v\n", line, err)
			continue
		}
		fresh, err := st.RecordOp(context.Background(), "sim", "sim", op.ItemID, op.Status)
		if err != nil {
			return err
		}
		if !fresh {
			dup++
			continue
		}
		if err := st.SetStatus(context.Background(), op.ItemID, op.Status); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "apply %s: %v\n", op.ItemID, err)
			continue
		}
		applied++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Printf("applied=%d duplicates=%d failed=%d\n", applied, dup, failed)
	return nil
}

func runPasswd(args []string) error {
	fs := flag.NewFlagSet("passwd", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return err
	}
	pw := strings.TrimSpace(string(raw))
	if pw == "" {
		return fmt.Errorf("empty password")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	srv, err := server.New(st, server.Config{})
	if err != nil {
		return err
	}
	if err := srv.SetPassword(context.Background(), pw); err != nil {
		return err
	}
	fmt.Println("password updated; every session was signed out")
	return nil
}
