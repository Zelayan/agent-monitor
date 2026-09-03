import * as vscode from 'vscode';

export interface CursorBridgeMessage {
  source: 'agent-monitor-web';
  action: 'focusAgent' | 'followUp' | 'openFile';
  payload?: {
    sessionId?: string;
    agent?: string;
    prompt?: string;
    filePath?: string;
  };
}

export class CursorBridge {
  /**
   * 处理来自 Webview 看板的命令与交互
   */
  public static async handleMessage(message: CursorBridgeMessage): Promise<void> {
    if (!message || message.source !== 'agent-monitor-web') {
      return;
    }

    switch (message.action) {
      case 'focusAgent':
        await this.focusAgent(message.payload);
        break;
      case 'followUp':
        await this.handleFollowUp(message.payload);
        break;
      case 'openFile':
        await this.openFile(message.payload?.filePath);
        break;
      default:
        break;
    }
  }

  /**
   * 聚焦 / 唤起 Cursor 内部的 Agent 或 Composer 面板
   */
  public static async focusAgent(payload?: { sessionId?: string; filePath?: string }): Promise<void> {
    // 1. 如果指定了本地文件（如 Transcript），直接在编辑器中打开
    if (payload?.filePath) {
      const opened = await this.openFile(payload.filePath);
      if (opened) return;
    }

    // 2. 依次尝试 Cursor Composer 与 AI Chat 相关命令
    const candidateCommands = [
      'composer.openComposer',
      'workbench.action.openComposer',
      'cursor.composer.open',
      'workbench.action.chat.open',
      'aichat.focus',
      'cursor.focusChat'
    ];

    for (const cmd of candidateCommands) {
      try {
        await vscode.commands.executeCommand(cmd);
        return;
      } catch (_) {
        // 命令不存在或版本差异，静默尝试下一个
      }
    }

    const shortcut = process.platform === 'darwin' ? '⌘I / ⌘L' : 'Ctrl+I / Ctrl+L';
    vscode.window.showInformationMessage(`已尝试唤起 Cursor 对话窗口。若未弹出，请按 ${shortcut} 激活。`);
  }

  /**
   * 处理追问与开启下一轮对话 (Follow-up)
   */
  public static async handleFollowUp(payload?: { prompt?: string; sessionId?: string }): Promise<void> {
    const prompt = (payload?.prompt || '').trim();
    if (!prompt) {
      return;
    }

    // 1. 写入系统剪贴板，方便即时粘贴
    try {
      await vscode.env.clipboard.writeText(prompt);
    } catch (_) {}

    // 2. 尝试通过 Cursor API 直接传递 Prompt 开启 Composer
    let dispatched = false;
    try {
      await vscode.commands.executeCommand('composer.startComposerPrompt', { prompt });
      dispatched = true;
    } catch (_) {
      // 兼容非直接支持参数的版本
    }

    if (!dispatched) {
      // 唤起对话窗口并提示
      await this.focusAgent();
    }

    vscode.window.showInformationMessage('✓ 追问内容已就绪（并复制到剪贴板），已在 Cursor 唤起对话窗口！');
  }

  /**
   * 在 VS Code / Cursor 编辑器中打开指定路径文件
   */
  public static async openFile(filePath?: string): Promise<boolean> {
    if (!filePath) return false;
    try {
      const uri = vscode.Uri.file(filePath);
      await vscode.commands.executeCommand('vscode.open', uri);
      return true;
    } catch (_) {
      return false;
    }
  }
}
