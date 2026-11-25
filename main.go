// SSHYard is a cross-platform SSH client with a native interface: hosts,
// tabs and split panes of terminals drawn by its own VT engine, and SFTP.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Licoy/sshyard/internal/app"
	"github.com/Licoy/sshyard/internal/awake"
	"github.com/Licoy/sshyard/internal/netwatch"
	"github.com/Licoy/sshyard/internal/portable"
	"github.com/Licoy/sshyard/internal/settings"
	"github.com/Licoy/sshyard/internal/theme"
	"github.com/Licoy/sshyard/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func themeSource(m theme.Mode) mygo.ThemeSource {
	switch m {
	case theme.Light:
		return mygo.ThemeLight
	case theme.Dark:
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// platform is the app's hooks into MyGo.
func platform() app.Platform {
	return app.Platform{
		Post:           postUI,
		ResolveStorage: storagePaths,
		Version:        mygo.App.Version(),
		Storage:        storage,
		CacheDir:       cacheDir,
		Updates:        updater.MygoBackend{},
		SetTheme:       func(m theme.Mode) { mygo.Theme.SetSource(themeSource(m)) },
		SetLocale:      mygo.App.SetLocale,
		OpenPath:       mygo.Shell.OpenPath,
		CopySecret: func(text string) error {
			return mygo.Clipboard.Write(transfer.TextData(text), mygo.ClipboardOptions{Concealed: true})
		},
		PickFile: func(title string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, ShowHiddenFiles: true})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		PickFiles: func(title string) ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Multiple: true, ShowHiddenFiles: true})
		},
		SaveFile: func(title, path string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Title: title, DefaultPath: path, CreateDirectories: true})
		},
		PickFolder: func(title, start string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, DefaultPath: start, Directory: true, CreateDirectories: true, ShowHiddenFiles: true})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		OpenWith:     openWith,
		OpenWindow:   openWindow,
		WatchNetwork: netwatch.Watch,
		Awake:        awake.System(),
		WatchSleep:   watchSleep,
	}
}

// watchSleep calls fn as the screen locks or the computer goes to sleep.
func watchSleep(fn func()) (stop func()) {
	offLock := mygo.Power.OnLockScreen(fn)
	offSleep := mygo.Power.OnSuspend(fn)
	return func() {
		offLock()
		offSleep()
	}
}

// storage is where the data of this copy may move, nil where it cannot;
// cacheDir is where a portable copy copies the files it opens in other
// apps. setupStorage sets them before the app runs.
var (
	storage  *app.Storage
	cacheDir string
)

// setupStorage keeps the data of a portable copy beside its executable,
// in place of the user's application data, after making the move the
// last launch asked for (portable.Apply).
func setupStorage() {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" || os.Getenv("SSHYARD_DATA_DIR") != "" {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	config, cerr := os.UserConfigDir()
	if err != nil || cerr != nil {
		return
	}
	dir, appData := filepath.Dir(exe), filepath.Join(config, mygo.App.Name())
	// The copy that asked quits as this one starts: wait for its lock.
	moveErr := portable.Apply(dir, appData, 15*time.Second)
	if moveErr != nil {
		log.Printf("sshyard: data move failed: %v", moveErr)
	}
	here := portable.Active(runtime.GOOS, dir)
	if here {
		d := portable.Path(dir)
		mygo.App.SetPath(mygo.PathUserData, d)
		mygo.App.SetPath(mygo.PathCache, filepath.Join(d, "cache"))
		mygo.App.SetPath(mygo.PathLogs, filepath.Join(d, "logs"))
		cacheDir = filepath.Join(d, "cache", "open")
	}
	if !portable.Supported(runtime.GOOS, dir) {
		return
	}
	storage = &app.Storage{Portable: here, PortableDir: portable.Path(dir), AppDataDir: appData, MoveErr: moveErr,
		Move: func() error {
			if err := portable.Request(dir, !here); err != nil {
				return err
			}
			mygo.App.Relaunch()
			return nil
		}}
}

func storagePaths() (app.StoragePaths, error) {
	canonical, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return app.StoragePaths{}, err
	}
	dir := os.Getenv("SSHYARD_DATA_DIR")
	if dir == "" {
		dir = canonical
	}
	return app.StoragePaths{DataDir: dir, LockPath: filepath.Join(canonical, "secrets.lock"), Scope: mygo.App.Name()}, nil
}

// updatePlugin is the update window, which checks soon after launch and
// at the interval of the saved settings while their automatic checks are
// on. It reads them before the app runs, so a new interval applies at the
// next launch; the app keeps the automatic checks in step after.
func updatePlugin() mygo.Plugin {
	s := settings.Default()
	if p, err := storagePaths(); err == nil {
		if saved, err := settings.Load(filepath.Join(p.DataDir, "settings.json")); err == nil {
			s = saved
		}
	}
	return updater.Plugin(time.Duration(s.UpdateIntervalHours)*time.Hour, s.CheckUpdates)
}

func main() {
	setupStorage()
	mygo.Use(updatePlugin())
	mygo.App.WhenReady(func() {
		// Startup failures are presented with safe, localized diagnostics in the UI.
		var d *desktop
		p := platform()
		p.SetTrayIcon = func(style string) error { return d.setTrayIcon(style) }
		p.SetRememberLayout = changeRememberLayout
		p.Notify = showNotification
		p.Relaunch = func() { mygo.App.Relaunch() }
		p.MainWindowBackground = func() bool {
			return d != nil && (!mygo.App.IsActive() || !d.window.IsFocused() || !d.window.IsVisible() || d.window.IsMinimized())
		}
		p.ActivateMainWindow = func() {
			d.show()
			d.window.Focus()
		}
		// The idle and sleep choices hide the windows, never lock them.
		p.HideWindows = func() {
			if d != nil && d.trayErr == nil {
				d.hide()
			}
		}
		a, _ := app.New(p)
		installWindowStates(a)
		mygo.App.OnQuit(a.Close)
		prepareWindowState("main")
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:     "SSHYard",
			Width:     1200,
			Height:    760,
			MinWidth:  820,
			MinHeight: 520,
			StateKey:  "main",
			// One unified toolbar: the page draws the title area, the
			// window controls sit over it.
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: 40,
			// The title bar draws the window's buttons, in the app's look
			// (app.WindowControls).
			HideWindowButtons: true,
			// A solid sidebar fill, without window material.
			Vibrancy:        mygo.VibrancyNone,
			BackgroundColor: "light-dark(#EDEDF0,#28282A)",
			// Native UI only: no web view anywhere in SSHYard.
			Content: ui.View(a.View),
		})
		rememberWindow(w, "main")
		d = newDesktop(a, w)
		a.Start()
		go updater.CleanLeftovers()
		mygo.App.OnDidResignActive(a.NotifyPendingApprovals)
		w.OnBlur(a.NotifyPendingApprovals)
		w.OnHide(a.NotifyPendingApprovals)
		w.OnMinimize(func() {
			a.SetMinimized(true)
			a.NotifyPendingApprovals()
		})
		w.OnRestore(func() { a.SetMinimized(false) })
		// Off the frame that clicked them: closing the window there
		// would take it down under the view.
		a.SetWindow(app.WindowControls{
			Minimize:   func() { go w.Minimize() },
			Maximize:   func() { go w.Maximize() },
			Unmaximize: func() { go w.Unmaximize() },
			Close:      func() { go w.Close() },
		})
		a.SetMaximized(w.IsMaximized())
		w.OnMaximize(func() { a.SetMaximized(true) })
		w.OnUnmaximize(func() { a.SetMaximized(false) })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func showNotification(ctx context.Context, note app.Notification) (func(), error) {
	n := mygo.NewNotification(mygo.NotificationOptions{ID: note.ID, Title: note.Title, Body: note.Body})
	if note.OnClick != nil {
		n.OnClick(note.OnClick)
	}
	if err := n.ShowContext(ctx); err != nil {
		// Remove the request from the native authorization queue, too.
		if ctx.Err() != nil {
			n.Close()
		}
		if errors.Is(err, mygo.ErrNotificationsDenied) {
			return nil, app.ErrNotificationsDenied
		}
		return nil, err
	}
	return n.Close, nil
}
