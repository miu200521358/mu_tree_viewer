//go:build windows
// +build windows

// 指示: miu200521358
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/miu200521358/mlib_go/pkg/adapter/io_common"
	"github.com/miu200521358/mlib_go/pkg/domain/model"
	"github.com/miu200521358/mlib_go/pkg/domain/motion"
	"github.com/miu200521358/mlib_go/pkg/infra/controller"
	"github.com/miu200521358/mlib_go/pkg/infra/controller/widget"
	sharedbase "github.com/miu200521358/mlib_go/pkg/shared/base"
	"github.com/miu200521358/mlib_go/pkg/shared/base/config"
	"github.com/miu200521358/mlib_go/pkg/shared/base/i18n"
	"github.com/miu200521358/mlib_go/pkg/shared/base/logging"
	"github.com/miu200521358/walk/pkg/walk"

	"github.com/miu200521358/mu_tree_viewer/pkg/adapter/mpresenter/messages"
	"github.com/miu200521358/mu_tree_viewer/pkg/usecase/minteractor"
)

const (
	treeViewerWindowIndex  = 0
	treeViewerModelIndex   = 0
	folderHistoryKey       = "folder"
	screenshotWaitTimeout  = 30 * time.Second
	screenshotPollInterval = 200 * time.Millisecond
)

// treeViewerState はmu_tree_viewerの画面状態を保持する。
type treeViewerState struct {
	translator i18n.II18n
	logger     logging.ILogger
	userConfig config.IUserConfig

	usecase *minteractor.TreeViewerUsecase

	window       *controller.ControlWindow
	player       *widget.MotionPlayer
	folderPicker *FolderPicker
	motionPicker *widget.FilePicker
	treeView     *TreeViewWidget

	folderPaths []string
	motionPath  string
	modelData   *model.PmxModel
	motionData  *motion.VmdMotion

	screenshotMu      sync.Mutex
	screenshotRunning bool
}

// newTreeViewerState は画面状態を初期化する。
func newTreeViewerState(translator i18n.II18n, logger logging.ILogger, userConfig config.IUserConfig, viewerUsecase *minteractor.TreeViewerUsecase) *treeViewerState {
	if logger == nil {
		logger = logging.DefaultLogger()
	}
	return &treeViewerState{
		translator: translator,
		logger:     logger,
		userConfig: userConfig,
		usecase:    viewerUsecase,
	}
}

// applyInitialPaths は初期パスをウィジェットに反映する。
func (s *treeViewerState) applyInitialPaths(initialMotionPath string) {
	if s == nil {
		return
	}
	if s.motionPicker != nil && initialMotionPath != "" {
		s.motionPicker.SetPath(initialMotionPath)
	}
}

// attachDropFiles はウィンドウのD&Dイベントを設定する。
func (s *treeViewerState) attachDropFiles(cw *controller.ControlWindow) {
	if s == nil || cw == nil {
		return
	}
	s.window = cw
	cw.DropFiles().Attach(func(files []string) {
		s.handleDropFiles(cw, files)
	})
}

// handleDropFiles はフォルダD&Dを処理する。
func (s *treeViewerState) handleDropFiles(cw *controller.ControlWindow, files []string) {
	if s == nil || len(files) == 0 {
		return
	}
	paths := cleanPaths(files)
	if len(paths) == 0 {
		return
	}
	if s.folderPicker != nil {
		s.folderPicker.SetPaths(paths)
		return
	}
	s.handleFolderPathsChanged(cw, paths)
}

// handleFolderPathsChanged はフォルダパス変更を処理する。
func (s *treeViewerState) handleFolderPathsChanged(cw *controller.ControlWindow, paths []string) {
	if s == nil {
		return
	}
	s.folderPaths = paths
	if s.treeView == nil {
		return
	}
	targetWindow := cw
	if targetWindow == nil {
		targetWindow = s.controlWindow()
	}
	setModelPaths := func() error {
		return s.treeView.SetModelPaths(paths)
	}
	err := error(nil)
	if targetWindow != nil {
		// ツリー構築中は再生時と同じ無効化で操作を抑止する。
		err = sharedbase.RunWithBoolState(targetWindow.SetEnabledInPlaying, true, targetWindow.Playing(), setModelPaths)
	} else {
		err = setModelPaths()
	}
	if err != nil {
		logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogTreeBuildFailure), err)
	}
	if len(paths) == 0 {
		return
	}
	if s.treeView.model == nil || s.treeView.model.RootCount() == 0 {
		logInfoLine(s.logger, i18n.TranslateOrMark(s.translator, messages.LogTreeEmpty))
	}
}

// handleMotionPathChanged はモーションパス変更を処理する。
func (s *treeViewerState) handleMotionPathChanged(cw *controller.ControlWindow, rep io_common.IFileReader, path string) {
	if s == nil {
		return
	}
	s.motionPath = path

	if s.usecase == nil {
		logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.MessageLoadFailed), nil)
		s.motionData = nil
		if cw != nil {
			cw.SetMotion(treeViewerWindowIndex, treeViewerModelIndex, nil)
		}
		s.updatePlayerStateWithFrame(nil, 0)
		return
	}
	motionResult, err := s.usecase.LoadMotion(rep, path)
	if err != nil {
		logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.MessageLoadFailed), err)
		s.motionData = nil
		if cw != nil {
			cw.SetMotion(treeViewerWindowIndex, treeViewerModelIndex, nil)
		}
		s.updatePlayerStateWithFrame(nil, 0)
		return
	}

	motionData := (*motion.VmdMotion)(nil)
	maxFrame := motion.Frame(0)
	if motionResult != nil {
		motionData = motionResult.Motion
		maxFrame = motionResult.MaxFrame
	}
	s.motionData = motionData
	if cw != nil {
		cw.SetMotion(treeViewerWindowIndex, treeViewerModelIndex, motionData)
	}
	s.updatePlayerStateWithFrame(motionData, maxFrame)
}

// handleTreeFileSelected はツリーで選択されたモデルを読み込む。
func (s *treeViewerState) handleTreeFileSelected(path string) {
	if s == nil || path == "" {
		return
	}
	if err := s.loadModelInternal(path, true); err != nil {
		logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.MessageLoadFailed), err)
	}
}

// loadModelInternal はモデルを読み込み、共有状態へ反映する。
func (s *treeViewerState) loadModelInternal(path string, logSuccess bool) error {
	if s == nil || path == "" {
		return fmt.Errorf("モデルパスが空です")
	}
	cw := s.controlWindow()
	load := func() error {
		if s.usecase == nil {
			return fmt.Errorf("モデル読み込み用のユースケースが未設定です")
		}
		result, err := s.usecase.LoadModel(nil, path)
		if err != nil {
			return err
		}
		modelData := (*model.PmxModel)(nil)
		if result != nil {
			modelData = result.Model
		}
		s.modelData = modelData
		if cw != nil {
			cw.SetModel(treeViewerWindowIndex, treeViewerModelIndex, modelData)
		}
		if logSuccess && modelData != nil {
			logInfoLine(s.logger, i18n.TranslateOrMark(s.translator, messages.LogLoadSuccess))
		}
		return nil
	}
	loadWithTreeViewGuard := func() error {
		return sharedbase.RunWithSetupTeardown(
			func() {
				if s.treeView != nil {
					s.treeView.SetEnabled(false)
				}
			},
			func() {
				if s.treeView != nil {
					s.treeView.SetEnabled(true)
					s.treeView.Focus()
				}
			},
			load,
		)
	}
	if cw == nil {
		return loadWithTreeViewGuard()
	}
	// モデル読み込み中は再生時と同じ無効化で操作を抑止する。
	return sharedbase.RunWithBoolState(cw.SetEnabledInPlaying, true, cw.Playing(), loadWithTreeViewGuard)
}

// handleCopyPath はパスコピーを処理する。
func (s *treeViewerState) handleCopyPath(path string) {
	if s == nil || path == "" {
		return
	}
	if err := walk.Clipboard().SetText(path); err != nil {
		logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogCopyFailure), err)
		return
	}
	logInfoLine(s.logger, i18n.TranslateOrMark(s.translator, messages.LogCopySuccess))
}

// handleScreenshotSave はスクリーンショット保存を開始する。
func (s *treeViewerState) handleScreenshotSave(path string, isDir bool) {
	if s == nil || path == "" {
		return
	}
	targets := s.collectScreenshotTargets(path, isDir)
	if len(targets) == 0 {
		logInfoLine(s.logger, i18n.TranslateOrMark(s.translator, messages.LogTreeEmpty))
		return
	}
	if !s.beginScreenshotSequence(targets) {
		if s.logger != nil {
			s.logger.Warn("スクリーンショット処理中のため新しい要求を無視しました")
		}
	}
}

// beginScreenshotSequence はスクリーンショット連続処理を開始する。
func (s *treeViewerState) beginScreenshotSequence(paths []string) bool {
	if s == nil || len(paths) == 0 {
		return false
	}
	s.screenshotMu.Lock()
	if s.screenshotRunning {
		s.screenshotMu.Unlock()
		return false
	}
	s.screenshotRunning = true
	s.screenshotMu.Unlock()

	go func() {
		defer func() {
			s.screenshotMu.Lock()
			s.screenshotRunning = false
			s.screenshotMu.Unlock()
		}()
		s.runScreenshotSequence(paths)
	}()
	return true
}

// runScreenshotSequence はスクリーンショットを順に保存する。
func (s *treeViewerState) runScreenshotSequence(paths []string) {
	if s == nil {
		return
	}
	cw := s.controlWindow()
	if cw == nil {
		return
	}
	for _, modelPath := range uniquePaths(paths) {
		if modelPath == "" {
			continue
		}
		if err := s.executeOnUIThread(func() error {
			return s.loadModelInternal(modelPath, true)
		}); err != nil {
			logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogScreenshotFailure), err)
			continue
		}

		screenshotPath, err := buildScreenshotPath(modelPath, time.Now())
		if err != nil {
			logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogScreenshotFailure), err)
			continue
		}
		requestID, reqErr := cw.RequestScreenshot(treeViewerWindowIndex, screenshotPath)
		if reqErr != nil {
			logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogScreenshotFailure), reqErr)
			continue
		}
		if waitErr := s.waitScreenshotResult(cw, requestID, screenshotWaitTimeout); waitErr != nil {
			logErrorWithTitle(s.logger, i18n.TranslateOrMark(s.translator, messages.LogScreenshotFailure), waitErr)
			continue
		}
		logInfoLine(s.logger, i18n.TranslateOrMark(s.translator, messages.LogScreenshotSuccess))
	}
}

// collectScreenshotTargets はスクリーンショット対象のモデルパス一覧を返す。
func (s *treeViewerState) collectScreenshotTargets(path string, isDir bool) []string {
	if s == nil || path == "" {
		return nil
	}
	if !isDir {
		return []string{path}
	}
	if s.treeView != nil {
		if paths := s.treeView.CollectModelPathsUnder(path); len(paths) > 0 {
			return paths
		}
	}
	paths, err := collectModelPaths(path)
	if err != nil && s.logger != nil {
		s.logger.Warn("スクリーンショット対象の探索に失敗しました: %s", err.Error())
	}
	return paths
}

// executeOnUIThread はUIスレッドで処理を実行して結果を返す。
func (s *treeViewerState) executeOnUIThread(action func() error) error {
	if s == nil {
		return fmt.Errorf("実行対象が未初期化です")
	}
	cw := s.controlWindow()
	if cw == nil {
		return action()
	}
	done := make(chan error, 1)
	cw.Synchronize(func() {
		done <- action()
	})
	return <-done
}

// waitScreenshotResult はスクリーンショット完了を待機する。
func (s *treeViewerState) waitScreenshotResult(cw *controller.ControlWindow, requestID uint64, timeout time.Duration) error {
	if cw == nil {
		return fmt.Errorf("スクリーンショット結果の取得に失敗しました")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if result, ok := cw.FetchScreenshotResult(requestID); ok {
			if result.ErrMessage != "" {
				return fmt.Errorf(result.ErrMessage)
			}
			return nil
		}
		time.Sleep(screenshotPollInterval)
	}
	return fmt.Errorf("スクリーンショット保存がタイムアウトしました")
}

// buildScreenshotPath はスクリーンショット保存先を生成する。
func buildScreenshotPath(modelPath string, now time.Time) (string, error) {
	if modelPath == "" {
		return "", fmt.Errorf("モデルパスが空です")
	}
	dir := filepath.Dir(modelPath)
	base := filepath.Base(modelPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		return "", fmt.Errorf("モデル名の取得に失敗しました")
	}
	stamp := now.Format("20060102150405")
	fileName := fmt.Sprintf("%s_screenshot_%s.png", name, stamp)
	return filepath.Join(dir, fileName), nil
}

// uniquePaths はパス一覧の重複を除去する。
func uniquePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

// updatePlayerStateWithFrame は再生UIを反映する。
func (s *treeViewerState) updatePlayerStateWithFrame(motionData *motion.VmdMotion, maxFrame motion.Frame) {
	if s == nil || s.player == nil {
		return
	}
	if motionData == nil {
		s.player.SetPlaying(false)
		s.player.Reset(0)
		return
	}
	if maxFrame <= 0 {
		maxFrame = motionData.MaxFrame()
	}
	s.player.Reset(maxFrame)
	s.player.SetPlaying(false)
}

// controlWindow は関連付けられたウィンドウを取得する。
func (s *treeViewerState) controlWindow() *controller.ControlWindow {
	if s == nil {
		return nil
	}
	return s.window
}
