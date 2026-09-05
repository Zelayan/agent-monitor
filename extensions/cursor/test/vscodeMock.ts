export interface MockWorkspaceFolder {
  uri: { fsPath: string };
  name: string;
  index: number;
}

export const workspace = {
  workspaceFolders: [] as MockWorkspaceFolder[] | undefined,
  getConfiguration: (_section: string) => ({
    get: <T>(_key: string, defaultValue: T): T => defaultValue,
  }),
};

export const window = {
  infoMessages: [] as string[],
  warningMessages: [] as string[],
  errorMessages: [] as string[],
  quickPickSelection: null as any,
  infoMessageSelection: null as string | null,

  showInformationMessage: async (msg: string, ...items: string[]): Promise<string | undefined> => {
    window.infoMessages.push(msg);
    if (window.infoMessageSelection && items.includes(window.infoMessageSelection)) {
      return window.infoMessageSelection;
    }
    return items[0];
  },
  showWarningMessage: async (msg: string, ...items: string[]): Promise<string | undefined> => {
    window.warningMessages.push(msg);
    return items[0];
  },
  showErrorMessage: async (msg: string, ...items: string[]): Promise<string | undefined> => {
    window.errorMessages.push(msg);
    return items[0];
  },
  showQuickPick: async (items: any[], _options?: any): Promise<any> => {
    if (window.quickPickSelection !== null) {
      return window.quickPickSelection;
    }
    return items[0];
  },
};

export const commands = {
  executedCommands: [] as { command: string; args: any[] }[],
  executeCommand: async (command: string, ...args: any[]): Promise<any> => {
    commands.executedCommands.push({ command, args });
    return true;
  },
};

export const Uri = {
  file: (fsPath: string) => ({ fsPath, scheme: 'file' }),
};

export function resetMock(): void {
  workspace.workspaceFolders = [];
  window.infoMessages = [];
  window.warningMessages = [];
  window.errorMessages = [];
  window.quickPickSelection = null;
  window.infoMessageSelection = null;
  commands.executedCommands = [];
}
