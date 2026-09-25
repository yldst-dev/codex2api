package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"codex-gateway/internal/store"
)

var errMenuBack = errors.New("menu back")

type menuKey int

const (
	menuNone menuKey = iota
	menuUp
	menuDown
	menuEnter
	menuQuit
)

func (a *app) mainMenu() error {
	items := []string{"계정", "API 키", "서버", "설정", "종료"}
	for {
		picked, err := a.choose(a.banner(), items)
		if err != nil || picked < 0 || picked == len(items)-1 {
			return err
		}
		var act error
		switch picked {
		case 0:
			act = a.authMenu()
		case 1:
			act = a.keyMenu()
		case 2:
			act = a.serverMenu()
		case 3:
			act = a.configMenu()
		}
		if act != nil && !errors.Is(act, errMenuBack) {
			fmt.Fprintf(a.out, "error: %s\n", act.Error())
			a.pause()
		}
	}
}

func (a *app) authMenu() error {
	items := []string{"브라우저 콜백 기다리기", "콜백 주소 붙여넣기", "상태", "토큰 갱신", "로그아웃", "뒤로"}
	for {
		picked, err := a.choose("로그인", items)
		if err != nil || picked < 0 || picked == len(items)-1 {
			return err
		}
		var act error
		switch picked {
		case 0:
			act = a.loginWith(false)
		case 1:
			act = a.loginWith(true)
		case 2:
			act = a.authStatus()
		case 3:
			act = a.authRefresh()
		case 4:
			act = a.logout()
		}
		if act != nil {
			fmt.Fprintf(a.out, "error: %s\n", act.Error())
		}
		a.pause()
	}
}

func (a *app) keyMenu() error {
	items := []string{"키 만들기", "목록", "폐기", "재발급", "뒤로"}
	for {
		picked, err := a.choose("API 키", items)
		if err != nil || picked < 0 || picked == len(items)-1 {
			return err
		}
		var act error
		switch picked {
		case 0:
			act = a.key([]string{"create"})
		case 1:
			act = a.key([]string{"list"})
		case 2:
			act = a.revokePicked()
		case 3:
			act = a.rotatePicked()
		}
		if errors.Is(act, errMenuBack) {
			continue
		}
		if act != nil {
			fmt.Fprintf(a.out, "error: %s\n", act.Error())
		}
		a.pause()
	}
}

func (a *app) serverMenu() error {
	items := []string{"시작", "상태", "뒤로"}
	for {
		picked, err := a.choose("서버", items)
		if err != nil || picked < 0 || picked == len(items)-1 {
			return err
		}
		var act error
		switch picked {
		case 0:
			act = a.server([]string{"start"})
		case 1:
			act = a.server([]string{"status"})
		}
		if act != nil {
			fmt.Fprintf(a.out, "error: %s\n", act.Error())
		}
		a.pause()
	}
}

func (a *app) configMenu() error {
	items := []string{"보기", "수신 주소 바꾸기", "뒤로"}
	for {
		picked, err := a.choose("설정", items)
		if err != nil || picked < 0 || picked == len(items)-1 {
			return err
		}
		var act error
		switch picked {
		case 0:
			act = a.configCmd([]string{"show"})
		case 1:
			act = a.configCmd([]string{"set", "listen"})
		}
		if act != nil {
			fmt.Fprintf(a.out, "error: %s\n", act.Error())
		}
		a.pause()
	}
}

func (a *app) revokePicked() error {
	id, err := a.pickKey("폐기할 키", true)
	if err != nil || id == "" {
		return err
	}
	picked, err := a.choose("이 키를 폐기할까요?", []string{"폐기", "취소"})
	if err != nil {
		return err
	}
	if picked != 0 {
		return errMenuBack
	}
	return a.key([]string{"revoke", id})
}

func (a *app) rotatePicked() error {
	id, err := a.pickKey("재발급할 키", true)
	if err != nil || id == "" {
		return err
	}
	picked, err := a.choose("이 키를 재발급할까요?", []string{"재발급", "취소"})
	if err != nil {
		return err
	}
	if picked != 0 {
		return errMenuBack
	}
	return a.key([]string{"rotate", id})
}

func (a *app) pickKey(title string, activeOnly bool) (string, error) {
	keys, err := a.keys.List(context.Background())
	if err != nil {
		return "", err
	}
	var labels []string
	var ids []string
	for _, key := range keys {
		if activeOnly && key.Status != store.KeyActive {
			continue
		}
		labels = append(labels, key.Name+"  "+key.Prefix+"  "+key.Status)
		ids = append(ids, key.ID)
	}
	if len(ids) == 0 {
		fmt.Fprintln(a.out, "선택할 API 키가 없습니다.")
		return "", errMenuBack
	}
	labels = append(labels, "뒤로")
	picked, err := a.choose(title, labels)
	if err != nil || picked < 0 || picked >= len(ids) {
		return "", err
	}
	return ids[picked], nil
}

func (a *app) banner() string {
	acc, err := a.tokens.Account(context.Background())
	state := "로그인 안 됨"
	if err == nil && acc != nil {
		if acc.Status == store.OAuthReauth {
			state = "다시 로그인 필요"
		} else if strings.TrimSpace(acc.Email) != "" {
			state = acc.Email
		} else {
			state = "로그인됨"
		}
	}
	return "Codex Gateway  ·  " + state + "  ·  " + a.cfg.Listen
}

func (a *app) prompt(label, fallback string) (string, error) {
	fmt.Fprintf(a.out, "%s [%s]: ", label, fallback)
	line, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

func (a *app) pause() {
	if !a.termOK {
		return
	}
	fmt.Fprint(a.out, "\nEnter를 누르면 계속합니다.")
	_, _ = bufio.NewReader(a.in).ReadString('\n')
}

func (a *app) choose(title string, items []string) (int, error) {
	if !a.termOK || len(items) == 0 {
		return -1, fmt.Errorf("menu needs a terminal")
	}
	fd := int(a.termIn.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return -1, err
	}
	defer term.Restore(fd, state)
	fmt.Fprint(a.termOut, "\x1b[?25l")
	defer fmt.Fprint(a.termOut, "\x1b[?25h\r\n")

	current := 0
	a.paint(title, items, current, false)
	for {
		key, err := readMenuKey(a.termIn)
		if err != nil {
			return -1, err
		}
		switch key {
		case menuUp:
			if current > 0 {
				current--
			}
		case menuDown:
			if current < len(items)-1 {
				current++
			}
		case menuEnter:
			return current, nil
		case menuQuit:
			return -1, nil
		default:
			continue
		}
		a.paint(title, items, current, true)
	}
}

func (a *app) paint(title string, items []string, current int, redraw bool) {
	if redraw {
		fmt.Fprintf(a.termOut, "\x1b[%dA", len(items)+4)
	} else {
		fmt.Fprint(a.termOut, "\x1b[H\x1b[2J")
	}
	fmt.Fprintf(a.termOut, "\x1b[2K%s\r\n\r\n", title)
	for i, item := range items {
		mark := "  "
		if i == current {
			mark = "> "
		}
		fmt.Fprintf(a.termOut, "\x1b[2K%s%s\r\n", mark, item)
	}
	fmt.Fprint(a.termOut, "\r\n\x1b[2K↑↓ 이동   Enter 선택   q 뒤로\r\n")
}

func readMenuKey(f *os.File) (menuKey, error) {
	var one [1]byte
	if _, err := f.Read(one[:]); err != nil {
		return menuNone, err
	}
	if one[0] != 0x1b {
		return classifyMenuKey(string(one[:])), nil
	}
	if !inputReady(f, 40*time.Millisecond) {
		return menuQuit, nil
	}
	var rest [8]byte
	n, _ := f.Read(rest[:])
	if n == 0 {
		return menuQuit, nil
	}
	return classifyMenuKey(string(one[:]) + string(rest[:n])), nil
}

func inputReady(f *os.File, wait time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, int(wait.Milliseconds()))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
	}
}

func classifyMenuKey(seq string) menuKey {
	switch seq {
	case "\x1b[A", "\x1bOA":
		return menuUp
	case "\x1b[B", "\x1bOB":
		return menuDown
	case "\r", "\n":
		return menuEnter
	case "q", "Q", "\x03", "\x1b":
		return menuQuit
	default:
		if strings.HasSuffix(seq, "A") && strings.Contains(seq, "\x1b") {
			return menuUp
		}
		if strings.HasSuffix(seq, "B") && strings.Contains(seq, "\x1b") {
			return menuDown
		}
		return menuNone
	}
}

func terminalReady(in io.Reader, out io.Writer) bool {
	_, _, ok := terminalFiles(in, out)
	return ok
}

func terminalFiles(in io.Reader, out io.Writer) (*os.File, *os.File, bool) {
	inf, okIn := in.(*os.File)
	outf, okOut := out.(*os.File)
	if !okIn || !okOut {
		return nil, nil, false
	}
	if !term.IsTerminal(int(inf.Fd())) || !term.IsTerminal(int(outf.Fd())) {
		return nil, nil, false
	}
	return inf, outf, true
}
