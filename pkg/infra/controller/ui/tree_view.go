//go:build windows
// +build windows

// 指示: miu200521358
package ui

import (
	"strings"

	"github.com/miu200521358/mlib_go/pkg/infra/controller"
	"github.com/miu200521358/mlib_go/pkg/shared/base/i18n"
	"github.com/miu200521358/mlib_go/pkg/shared/base/logging"
	"github.com/miu200521358/walk/pkg/declarative"
	"github.com/miu200521358/walk/pkg/walk"

	"github.com/miu200521358/mu_tree_viewer/pkg/adapter/mpresenter/messages"
)

// TreeViewWidget はツリービュー表示のウィジェットを表す。
type TreeViewWidget struct {
	container         *walk.Composite
	treeView          *walk.TreeView
	model             *TreeModel
	translator        i18n.II18n
	logger            logging.ILogger
	minSize           declarative.Size
	maxSize           declarative.Size
	stretchFactor     int
	contextPath       string
	contextCopy       *walk.Action
	contextScreenshot *walk.Action
	contextIsDir      bool
	lastSelected      string
	pendingKey        walk.Key
	pendingBase       string
	pendingActive     bool
	onFileSelected    func(string)
	onCopyPath        func(string)
	onScreenshotSave  func(string, bool)
}

// NewTreeViewWidget はTreeViewWidgetを生成する。
func NewTreeViewWidget(translator i18n.II18n, logger logging.ILogger, onFileSelected func(string), onCopyPath func(string), onScreenshotSave func(string, bool)) *TreeViewWidget {
	if logger == nil {
		logger = logging.DefaultLogger()
	}
	return &TreeViewWidget{
		translator:       translator,
		logger:           logger,
		model:            NewTreeModel(),
		onFileSelected:   onFileSelected,
		onCopyPath:       onCopyPath,
		onScreenshotSave: onScreenshotSave,
	}
}

// SetMinSize は最小サイズを設定する。
func (tw *TreeViewWidget) SetMinSize(size declarative.Size) {
	if tw == nil {
		return
	}
	tw.minSize = size
}

// SetMaxSize は最大サイズを設定する。
func (tw *TreeViewWidget) SetMaxSize(size declarative.Size) {
	if tw == nil {
		return
	}
	tw.maxSize = size
}

// SetStretchFactor は伸長率を設定する。
func (tw *TreeViewWidget) SetStretchFactor(factor int) {
	if tw == nil {
		return
	}
	tw.stretchFactor = factor
}

// SetWindow はウィンドウ参照を設定する（TreeViewは未使用）。
func (tw *TreeViewWidget) SetWindow(_ *controller.ControlWindow) {
	if tw == nil {
		return
	}
	if tw.container != nil {
		tw.container.Synchronize(func() {
			tw.updateLayout()
		})
	}
}

// SetEnabledInPlaying は再生中の有効状態を設定する。
func (tw *TreeViewWidget) SetEnabledInPlaying(playing bool) {
	if tw == nil || tw.treeView == nil {
		return
	}
	// 再生中も選択変更を許可する。
	tw.treeView.SetEnabled(true)
}

// SetEnabled はツリービューの有効状態を設定する。
func (tw *TreeViewWidget) SetEnabled(enabled bool) {
	if tw == nil || tw.treeView == nil {
		return
	}
	tw.treeView.SetEnabled(enabled)
}

// SetModelPaths はルートパス一覧からツリーを再構築する。
func (tw *TreeViewWidget) SetModelPaths(paths []string) error {
	if tw == nil {
		return nil
	}
	if tw.model == nil {
		tw.model = NewTreeModel()
		if tw.treeView != nil {
			if err := tw.treeView.SetModel(tw.model); err != nil {
				return err
			}
		}
	}
	if err := tw.model.SetRoots(paths); err != nil {
		tw.updateLayout()
		return err
	}
	tw.updateLayout()
	if tw.treeView != nil && tw.model != nil && tw.model.RootCount() > 0 {
		// フォルダ読み込み直後は全展開して操作負荷を下げる。
		tw.expandAllDirNodes()
	}
	return nil
}

// expandAllDirNodes はディレクトリノードのみを全展開する。
func (tw *TreeViewWidget) expandAllDirNodes() {
	if tw == nil || tw.treeView == nil || tw.model == nil {
		return
	}
	tw.treeView.SetSuspended(true)
	defer tw.treeView.SetSuspended(false)

	stack := make([]*TreeNode, 0, 64)
	for _, root := range tw.model.roots {
		if root == nil {
			continue
		}
		stack = append(stack, root)
	}

	for len(stack) > 0 {
		idx := len(stack) - 1
		node := stack[idx]
		stack = stack[:idx]
		if node == nil || !node.IsDir() || len(node.children) == 0 {
			continue
		}
		_ = tw.treeView.SetExpanded(node, true)
		for i := len(node.children) - 1; i >= 0; i-- {
			child := node.children[i]
			if child == nil || !child.IsDir() || len(child.children) == 0 {
				continue
			}
			stack = append(stack, child)
		}
	}
}

// Widgets はUI構成を返す。
func (tw *TreeViewWidget) Widgets() declarative.Composite {
	return declarative.Composite{
		Layout: declarative.VBox{},
		Children: []declarative.Widget{
			// 内側でオーバーレイ配置を行うため、外側はサイズ確保用に分離する。
			declarative.Composite{
				AssignTo:      &tw.container,
				Layout:        declarative.VBox{MarginsZero: true, SpacingZero: true},
				StretchFactor: tw.stretchFactor,
				MinSize:       tw.minSize,
				MaxSize:       tw.maxSize,
				OnSizeChanged: func() {
					tw.updateLayout()
				},
				OnBoundsChanged: func() {
					tw.updateLayout()
				},
				Children: []declarative.Widget{
					declarative.TreeView{
						AssignTo:      &tw.treeView,
						Model:         tw.model,
						StretchFactor: 1,
						ToolTipText:   i18n.TranslateOrMark(tw.translator, messages.LabelTreeViewTip),
						ContextMenuItems: []declarative.MenuItem{
							declarative.Action{
								AssignTo:    &tw.contextCopy,
								Text:        i18n.TranslateOrMark(tw.translator, messages.LabelCopyFullPath),
								Enabled:     false,
								OnTriggered: tw.handleContextCopy,
							},
							declarative.Action{
								AssignTo:    &tw.contextScreenshot,
								Text:        i18n.TranslateOrMark(tw.translator, messages.LabelScreenshotSave),
								Enabled:     false,
								OnTriggered: tw.handleContextScreenshotSave,
							},
						},
						OnCurrentItemChanged: tw.handleCurrentItemChanged,
						OnKeyDown:            tw.handleKeyDown,
						OnKeyUp:              tw.handleKeyUp,
						OnMouseDown: func(x, y int, button walk.MouseButton) {
							tw.handleMouseDown(x, y, button)
						},
					},
				},
			},
		},
	}
}

// updateLayout は内部ウィジェットのサイズを調整する。
func (tw *TreeViewWidget) updateLayout() {
	if tw == nil || tw.container == nil || tw.treeView == nil {
		return
	}
	bounds := tw.container.ClientBounds()
	tw.treeView.SetBounds(bounds)
}

// handleCurrentItemChanged は選択変更時の処理を行う。
func (tw *TreeViewWidget) handleCurrentItemChanged() {
	if tw == nil || tw.treeView == nil {
		return
	}
	item := tw.treeView.CurrentItem()
	node, ok := item.(*TreeNode)
	if !ok || node == nil || node.IsDir() {
		return
	}
	tw.lastSelected = node.Path()
	if tw.onFileSelected != nil {
		tw.onFileSelected(node.Path())
	}
}

// handleMouseDown はクリック時の処理を行う。
func (tw *TreeViewWidget) handleMouseDown(x, y int, button walk.MouseButton) {
	if tw == nil {
		return
	}
	if button != walk.RightButton {
		return
	}
	tw.prepareContextMenu(x, y)
}

// prepareContextMenu は右クリック時のコンテキストメニュー状態を更新する。
func (tw *TreeViewWidget) prepareContextMenu(x, y int) {
	if tw == nil || tw.treeView == nil {
		return
	}
	item := tw.treeView.ItemAt(x, y)
	node, ok := item.(*TreeNode)
	if !ok || node == nil {
		tw.updateContextMenu("", false)
		return
	}
	// 右クリック時はモデル読み込みを避けるため選択変更は行わない。
	tw.updateContextMenu(node.Path(), node.IsDir())
}

// updateContextMenu はコンテキストメニューの有効状態を更新する。
func (tw *TreeViewWidget) updateContextMenu(path string, isDir bool) {
	if tw == nil {
		return
	}
	tw.contextPath = path
	tw.contextIsDir = isDir
	enabled := path != ""
	tw.setActionEnabled(tw.contextCopy, enabled && !isDir)
	tw.setActionEnabled(tw.contextScreenshot, enabled)
}

// setActionEnabled はアクションの有効状態を設定する。
func (tw *TreeViewWidget) setActionEnabled(action *walk.Action, enabled bool) {
	if action == nil {
		return
	}
	if err := action.SetEnabled(enabled); err != nil && tw.logger != nil {
		tw.logger.Warn("メニュー状態の更新に失敗しました: %s", err.Error())
	}
}

// handleContextCopy はコンテキストメニューのパスコピー処理を行う。
func (tw *TreeViewWidget) handleContextCopy() {
	if tw == nil || tw.contextPath == "" {
		return
	}
	if tw.onCopyPath != nil {
		tw.onCopyPath(tw.contextPath)
	}
}

// handleContextScreenshotSave はスクリーンショット保存を実行する。
func (tw *TreeViewWidget) handleContextScreenshotSave() {
	if tw == nil || tw.contextPath == "" {
		return
	}
	if tw.onScreenshotSave != nil {
		tw.onScreenshotSave(tw.contextPath, tw.contextIsDir)
	}
}

// CollectModelPathsUnder は指定パス配下のモデルパスを収集する。
func (tw *TreeViewWidget) CollectModelPathsUnder(path string) []string {
	if tw == nil || tw.model == nil || path == "" {
		return nil
	}
	node := findNodeByPath(tw.model.roots, path)
	if node == nil {
		return nil
	}
	nodes := make([]*TreeNode, 0, 64)
	collectFileNodesRecursive(node, &nodes)
	return extractNodePaths(nodes)
}

// handleKeyDown はキー操作の起点を記録する。
func (tw *TreeViewWidget) handleKeyDown(key walk.Key) {
	if tw == nil {
		return
	}
	switch key {
	case walk.KeyDown, walk.KeyUp:
		base := tw.lastSelected
		if base == "" {
			base = tw.resolveCurrentFilePath()
		}
		tw.pendingKey = key
		tw.pendingBase = base
		tw.pendingActive = true
	}
}

// handleKeyUp はツリービューのキー操作を処理する。
func (tw *TreeViewWidget) handleKeyUp(key walk.Key) {
	if tw == nil {
		return
	}
	base := ""
	if tw.pendingActive && tw.pendingKey == key {
		base = tw.pendingBase
	}
	tw.pendingKey = 0
	tw.pendingBase = ""
	tw.pendingActive = false

	switch key {
	case walk.KeyDown:
		tw.moveSelectionByDelta(1, base)
	case walk.KeyUp:
		tw.moveSelectionByDelta(-1, base)
	}
}

// moveSelectionByDelta は上下キーでモデル選択を進める。
func (tw *TreeViewWidget) moveSelectionByDelta(delta int, basePath string) {
	if tw == nil || tw.treeView == nil || tw.model == nil {
		return
	}
	nodes := collectFileNodes(tw.model.roots)
	if len(nodes) == 0 {
		return
	}
	currentIndex := resolveFileNodeIndex(nodes, basePath)
	// 基準パスが無い場合は直近選択/現在選択を補助的に使う。
	if currentIndex < 0 {
		currentIndex = resolveFileNodeIndex(nodes, tw.lastSelected)
	}
	if currentIndex < 0 {
		if current := tw.resolveCurrentFilePath(); current != "" {
			currentIndex = resolveFileNodeIndex(nodes, current)
		}
	}
	targetIndex := currentIndex + delta
	if currentIndex < 0 {
		if delta < 0 {
			targetIndex = len(nodes) - 1
		} else {
			targetIndex = 0
		}
	}
	if targetIndex < 0 {
		targetIndex = 0
	}
	if targetIndex >= len(nodes) {
		targetIndex = len(nodes) - 1
	}
	target := nodes[targetIndex]
	if target == nil {
		return
	}
	tw.selectFileNode(target)
}

// resolveCurrentFilePath は現在選択されているファイルパスを返す。
func (tw *TreeViewWidget) resolveCurrentFilePath() string {
	if tw == nil || tw.treeView == nil {
		return ""
	}
	item := tw.treeView.CurrentItem()
	node, ok := item.(*TreeNode)
	if !ok || node == nil || node.IsDir() {
		return ""
	}
	return node.Path()
}

// selectFileNode はモデルノードを選択して表示位置を合わせる。
func (tw *TreeViewWidget) selectFileNode(node *TreeNode) {
	if tw == nil || tw.treeView == nil || node == nil {
		return
	}
	tw.expandAncestors(node)
	if err := tw.treeView.SetCurrentItem(node); err != nil && tw.logger != nil {
		tw.logger.Warn("ツリー選択の更新に失敗しました: %s", err.Error())
		return
	}
	if err := tw.treeView.EnsureVisible(node); err != nil && tw.logger != nil {
		tw.logger.Warn("ツリー表示の更新に失敗しました: %s", err.Error())
	}
}

// expandAncestors は親ノードを展開して表示対象を可視化する。
func (tw *TreeViewWidget) expandAncestors(node *TreeNode) {
	if tw == nil || tw.treeView == nil || node == nil {
		return
	}
	current := node.parent
	for current != nil {
		_ = tw.treeView.SetExpanded(current, true)
		current = current.parent
	}
}

// collectFileNodes は表示順でファイルノードを収集する。
func collectFileNodes(roots []*TreeNode) []*TreeNode {
	if len(roots) == 0 {
		return nil
	}
	nodes := make([]*TreeNode, 0, 128)
	for _, root := range roots {
		collectFileNodesRecursive(root, &nodes)
	}
	return nodes
}

// collectFileNodesRecursive はツリーを走査してファイルノードを追加する。
func collectFileNodesRecursive(node *TreeNode, out *[]*TreeNode) {
	if node == nil {
		return
	}
	if !node.IsDir() {
		*out = append(*out, node)
		return
	}
	for _, child := range node.children {
		collectFileNodesRecursive(child, out)
	}
}

// extractNodePaths はノード一覧からパスを抽出する。
func extractNodePaths(nodes []*TreeNode) []string {
	if len(nodes) == 0 {
		return nil
	}
	paths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		path := node.Path()
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// findNodeByPath は指定パスに一致するノードを探索する。
func findNodeByPath(nodes []*TreeNode, path string) *TreeNode {
	if len(nodes) == 0 || path == "" {
		return nil
	}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if sameFilePath(node.Path(), path) {
			return node
		}
		if node.IsDir() {
			if found := findNodeByPath(node.children, path); found != nil {
				return found
			}
		}
	}
	return nil
}

// resolveFileNodeIndex は指定パスに一致するノードの位置を返す。
func resolveFileNodeIndex(nodes []*TreeNode, path string) int {
	if len(nodes) == 0 || path == "" {
		return -1
	}
	for i, node := range nodes {
		if node == nil {
			continue
		}
		if sameFilePath(node.Path(), path) {
			return i
		}
	}
	return -1
}

// sameFilePath は大文字小文字を無視してパスが一致するか判定する。
func sameFilePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(a, b)
}
