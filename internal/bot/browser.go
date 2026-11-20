package bot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// BrowserOptions control how Chrome is launched for one meeting.
type BrowserOptions struct {
	ChromePath string // empty = auto-detect
	// Headless is only for tests against the local fake Meet page.
	// Real Google Meet blocks headless Chrome, so production runs headed (on Xvfb on Linux).
	Headless   bool
	ExtraFlags map[string]any
	Log        func(msg string, args ...any)
}

// Browser is one Chrome process with one tab, using a throwaway profile.
type Browser struct {
	Ctx        context.Context // tab context for chromedp actions
	profileDir string
	cancel     []context.CancelFunc
}

// Launch starts Chrome with the flags from docs/PLAN.md §4.
func Launch(parent context.Context, o BrowserOptions) (*Browser, error) {
	profile, err := os.MkdirTemp("", "meeting-agent-chrome-")
	if err != nil {
		return nil, err
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserDataDir(profile),
		chromedp.WindowSize(1280, 720),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("accept-lang", "en-US"),
		// AudioContext must start without a click.
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-features", "Translate"),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
	}
	if os.Geteuid() == 0 {
		opts = append(opts, chromedp.NoSandbox) // Chrome refuses to run as root with the sandbox
	}
	if o.Headless {
		opts = append(opts, chromedp.Headless)
	}
	if p := o.ChromePath; p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	} else if p := detectChrome(); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	for k, v := range o.ExtraFlags {
		opts = append(opts, chromedp.Flag(k, v))
	}

	b := &Browser{profileDir: profile}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	var ctxOpts []chromedp.ContextOption
	if o.Log != nil {
		ctxOpts = append(ctxOpts, chromedp.WithErrorf(func(f string, a ...any) { o.Log("chromedp: " + fmt.Sprintf(f, a...)) }))
	}
	tabCtx, cancelTab := chromedp.NewContext(allocCtx, ctxOpts...)
	b.cancel = []context.CancelFunc{cancelTab, cancelAlloc}
	b.Ctx = tabCtx

	// Start the browser now so launch errors surface here, and deny camera/mic:
	// the bot only listens and never sends media.
	// The first chromedp call starts the browser and ties its lifetime to that
	// call's context, so it must use the tab context itself, not a timeout child.
	if err = chromedp.Do(tabCtx); err != nil {
		b.Close()
		return nil, fmt.Errorf("launch chrome: %w", err)
	}
	startCtx, cancel := context.WithTimeout(tabCtx, 30*time.Second)
	defer cancel()
	for _, name := range []string{"microphone", "camera"} {
		_, err = chromedp.CallBrowser(startCtx, browser.SetPermission, browser.SetPermissionParams{
			Permission: &browser.PermissionDescriptor{Name: name},
			Setting:    browser.PermissionSettingDenied,
		})
		if err != nil {
			break
		}
	}
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("launch chrome: %w", err)
	}
	return b, nil
}

// Close kills Chrome and deletes the temporary profile.
func (b *Browser) Close() {
	for _, c := range b.cancel {
		c()
	}
	// Chrome can hold files for a moment after exit.
	for i := 0; i < 5; i++ {
		if os.RemoveAll(b.profileDir) == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func detectChrome() string {
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
