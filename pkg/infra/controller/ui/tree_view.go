//go:build windows
// +build windows

// 指示: miu200521358
package ui

import (
	"github.com/miu200521358/mlib_go/pkg/infra/controller"
	"github.com/miu200521358/mlib_go/pkg/shared/base/i18n"
	"github.com/miu200521358/mlib_go/pkg/shared/base/logging"
	"github.com/miu200521358/walk/pkg/declarative"
	"github.com/miu200521358/walk/pkg/walk"

	"github.com/miu200521358/mu_tree_viewer/pkg/adapter/mpresenter/messages"
)

// TreeViewWidget はツリービュー表示のウィジェットを表す。
type TreeViewWidget struct {
	container      *walk.Composite
	treeView       *walk.TreeView
	model          *TreeModel
	translator     i18n.II18n
	logger         logging.ILogger
	minSize        declarative.Size
	maxSize        declarative.Size
	stretchFactor  int
	contextPath    string
	contextCopy    *walk.Action
	contextSendTo  *walk.Action
	onFileSelected func(string)
	onCopyPath     func(string)
	onSendToPath   func(string)
}

// NewTreeViewWidget はTreeViewWidgetを生成する。
func NewTreeViewWidget(translator i18n.II18n, logger logging.ILogger, onFileSelected func(string), onCopyPath func(string), onSendToPath func(string)) *TreeViewWidget {
	if logger == nil {
		logger = logging.DefaultLogger()
	}
	return &TreeViewWidget{
		translator:     translator,
		logger:         logger,
		model:          NewTreeModel(),
		onFileSelected: onFileSelected,
		onCopyPath:     onCopyPath,
		onSendToPath:   onSendToPath,
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
								AssignTo:    &tw.contextSendTo,
								Text:        i18n.TranslateOrMark(tw.translator, messages.LabelSendTo),
								Enabled:     false,
								OnTriggered: tw.handleContextSendTo,
							},
						},
						OnCurrentItemChanged: tw.handleCurrentItemChanged,
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
	if !ok || node == nil || node.IsDir() {
		tw.updateContextMenu("")
		return
	}
	// 右クリック時はモデル読み込みを避けるため選択変更は行わない。
	tw.updateContextMenu(node.Path())
}

// updateContextMenu はコンテキストメニューの有効状態を更新する。
func (tw *TreeViewWidget) updateContextMenu(path string) {
	if tw == nil {
		return
	}
	tw.contextPath = path
	enabled := path != ""
	tw.setActionEnabled(tw.contextCopy, enabled)
	tw.setActionEnabled(tw.contextSendTo, enabled)
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

// handleContextSendTo はコンテキストメニューの送る処理を行う。
func (tw *TreeViewWidget) handleContextSendTo() {
	if tw == nil || tw.contextPath == "" {
		return
	}
	if tw.onSendToPath != nil {
		tw.onSendToPath(tw.contextPath)
	}
}
