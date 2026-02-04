//go:build windows
// +build windows

// 指示: miu200521358
package ui

import (
	"errors"
	"syscall"

	"github.com/miu200521358/mlib_go/pkg/shared/base/logging"
	"github.com/miu200521358/win"
)

// logInfoLine は情報ログを1行として出力する。
func logInfoLine(logger logging.ILogger, message string, params ...any) {
	if logger == nil {
		logger = logging.DefaultLogger()
	}
	if lineLogger, ok := logger.(interface {
		InfoLine(msg string, params ...any)
	}); ok {
		lineLogger.InfoLine(message, params...)
		return
	}
	logger.Info(message, params...)
}

// logErrorWithTitle はタイトル付きのエラーログを出力する。
func logErrorWithTitle(logger logging.ILogger, title string, err error) {
	if logger == nil {
		logger = logging.DefaultLogger()
	}
	if err == nil {
		if titled, ok := logger.(interface {
			ErrorTitle(title string, err error, msg string, params ...any)
		}); ok {
			titled.ErrorTitle(title, nil, "")
			return
		}
		logger.Error(title)
		return
	}
	if titled, ok := logger.(interface {
		ErrorTitle(title string, err error, msg string, params ...any)
	}); ok {
		titled.ErrorTitle(title, err, "")
		return
	}
	logger.Error("%s: %s", title, err.Error())
}

// sendToPath はWindowsの送る機能を呼び出す。
func sendToPath(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	verb, err := syscall.UTF16PtrFromString("sendto")
	if err != nil {
		return err
	}
	filePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if ok := win.ShellExecute(0, verb, filePath, nil, nil, win.SW_SHOWNORMAL); !ok {
		return errors.New("sendto failed")
	}
	return nil
}
