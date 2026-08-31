// Package browser provides a user-authorized, persistent browser session.
//
// The browser is deliberately kept as the holder of authentication state. The
// CLI does not collect credentials, inspect the user's normal browser profile,
// or copy cookies out of the browser into its own HTTP client.
package browser

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	profileAppDir  = "swjtu-cli"
	profileDirName = "browser-profile"
	browserEnv     = "SWJTU_CLI_BROWSER"
)

// Options controls how a browser session is created.
type Options struct {
	// BrowserPath overrides browser discovery. It may be an executable path or
	// a name available on PATH. SWJTU_CLI_BROWSER is used when this is empty.
	BrowserPath string
	// ProfileDir is the dedicated profile directory used for the session. If
	// empty, DefaultProfileDir is used.
	ProfileDir string
}

// Page is a snapshot of the active browser tab.
type Page struct {
	URL   string
	Title string
	HTML  string
}

// Session owns one browser process and one tab. Authentication state is kept
// in the browser's dedicated profile directory between CLI invocations.
type Session struct {
	ctx             context.Context
	cancel          context.CancelFunc
	allocatorCancel context.CancelFunc
	closeOnce       sync.Once
	profileDir      string
	browserPath     string
}

// DefaultProfileDir returns the dedicated profile directory used by SWJTU-CLI.
// It never points at the user's normal Chrome/Chromium profile.
func DefaultProfileDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, profileAppDir, profileDirName), nil
}

// ValidateTargetURL accepts only web URLs that can participate in a normal
// browser session. Local files and non-web schemes are intentionally rejected.
func ValidateTargetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("URL must include a host")
	}
	return nil
}

// ValidateSWJTUTargetURL restricts authenticated browser navigation to SWJTU
// hosts (plus localhost for local development). This prevents an accidental
// auth command from opening an unrelated site in the profile that holds the
// user's university login state.
func ValidateSWJTUTargetURL(raw string) error {
	if err := ValidateTargetURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host != "localhost" && host != "swjtu.edu.cn" && !strings.HasSuffix(host, ".swjtu.edu.cn") {
		return fmt.Errorf("认证浏览器只允许访问 swjtu.edu.cn 域名")
	}
	return nil
}

// New starts a headed browser using a dedicated persistent profile.
func New(opts Options) (*Session, error) {
	profileDir := opts.ProfileDir
	if profileDir == "" {
		var err error
		profileDir, err = DefaultProfileDir()
		if err != nil {
			return nil, err
		}
	}
	profileDir, err := filepath.Abs(profileDir)
	if err != nil {
		return nil, fmt.Errorf("resolve browser profile: %w", err)
	}
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return nil, fmt.Errorf("create browser profile: %w", err)
	}
	if err := os.Chmod(profileDir, 0700); err != nil {
		return nil, fmt.Errorf("protect browser profile: %w", err)
	}

	browserPath, err := findBrowser(opts.BrowserPath)
	if err != nil {
		return nil, err
	}

	allocatorOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOpts = append(allocatorOpts,
		chromedp.ExecPath(browserPath),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("window-size", "1280,900"),
	)

	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), allocatorOpts...)
	taskCtx, cancel := chromedp.NewContext(allocatorCtx)

	return &Session{
		ctx:             taskCtx,
		cancel:          cancel,
		allocatorCancel: allocatorCancel,
		profileDir:      profileDir,
		browserPath:     browserPath,
	}, nil
}

// Open navigates the active tab and waits briefly for client-side rendering.
func (s *Session) Open(targetURL string, settle time.Duration) (Page, error) {
	if err := ValidateTargetURL(targetURL); err != nil {
		return Page{}, err
	}

	actions := []chromedp.Action{
		chromedp.Navigate(targetURL),
		chromedp.WaitReady("body"),
	}
	if settle > 0 {
		actions = append(actions, chromedp.Sleep(settle))
	}
	if err := chromedp.Run(s.ctx, actions...); err != nil {
		return Page{}, fmt.Errorf("open page: %w", err)
	}
	return s.CurrentPage()
}

// CurrentPage returns the current URL, title, and rendered HTML of the active
// tab. HTML is collected from the page after JavaScript has run, so it also
// works for many SPA-based sites.
func (s *Session) CurrentPage() (Page, error) {
	var page Page
	if err := chromedp.Run(s.ctx,
		chromedp.Location(&page.URL),
		chromedp.Title(&page.Title),
		chromedp.OuterHTML("html", &page.HTML),
	); err != nil {
		return Page{}, fmt.Errorf("read page: %w", err)
	}
	return page, nil
}

// ProfileDir returns the profile path used by this session.
func (s *Session) ProfileDir() string {
	return s.profileDir
}

// BrowserPath returns the executable used by this session.
func (s *Session) BrowserPath() string {
	return s.browserPath
}

// Close closes the browser process and its tab. The profile itself is kept so
// the user can explicitly reuse the login session later.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.allocatorCancel()
	})
}

func findBrowser(configured string) (string, error) {
	var candidates []string
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if envPath := os.Getenv(browserEnv); envPath != "" && envPath != configured {
		candidates = append(candidates, envPath)
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		)
	case "windows":
		candidates = append(candidates,
			os.Getenv("PROGRAMFILES")+`\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES(X86)")+`\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LOCALAPPDATA")+`\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES")+`\Microsoft\Edge\Application\msedge.exe`,
		)
	default:
		candidates = append(candidates,
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
		)
	}

	seen := make(map[string]bool)
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true

		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("未找到 Chrome/Chromium，请安装浏览器或设置 %s", browserEnv)
}

// LooksLikeCASLogin reports whether a page is the SWJTU CAS login page. It is
// used to give the user a chance to re-authenticate when a saved session has
// expired.
func LooksLikeCASLogin(pageURL string) bool {
	u, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	if host == "cas.swjtu.edu.cn" {
		return strings.Contains(path, "/authserver/login")
	}
	return host == "identity.swjtu.edu.cn" && (path == "/login" || strings.Contains(path, "/authserver/login"))
}
