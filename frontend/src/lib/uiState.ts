// Explicit visible UI fields; each mounted controller receives an isolated instance.
export interface UiFieldValues {
  'RequireAuth.state': 'checking' | 'ok' | 'login' | 'setup';
  'Sidebar.username': string;
  'Sidebar.authEnabled': boolean;
  'NodeNameProvider.nodeName': string;
  'Dashboard.refreshTick': number;
  'Dashboard.animateCharts': boolean;
  'Dashboard.observedLowerSections': boolean;
  'Logs.refreshInterval': number;
  'Logs.page': { filterKey: string; offset: number };
  'Logs.logs': import('../api/generated').QueryLogView[];
  'Logs.total': number;
  'Logs.loading': boolean;
  'Logs.error': string | null;
  'Logs.expandedId': number | null;
  'Logs.evalDetail': import('../api/generated').QueryLogDetailView | null;
  'Logs.evalLoading': boolean;
  'Logs.contextMenu': {
    x: number;
    y: number;
    entry: import('../api/generated').QueryLogView;
  } | null;
  'Filters.addUrl': string;
  'Filters.addName': string;
  'ListsTab.editingId': number | null;
  'ListsTab.editName': string;
  'HistoryTab.listFilter': number | null;
  'HistoryTab.search': string;
  'DetailsTab.selectedCheckpoint': number | null;
  'RecentActivityWidget.expandedId': number | null;
  'RecentActivityWidget.evalDetail': import('../api/generated').QueryLogDetailView | null;
  'RecentActivityWidget.evalLoading': boolean;
  'RecentActivityWidget.contextMenu': {
    x: number;
    y: number;
    log: import('../api/generated').RecentLogView;
  } | null;
  'ListToggleSection.collapsed': boolean;
  'PolicyDetailView.editName': string;
  'PolicyDetailView.editDesc': string;
  'PolicyDetailView.editing': boolean;
  'PolicyDetailView.newRule': string;
  'PolicyDetailView.ruleType': 'block' | 'allow';
  'PolicyDetailView.confirmDelete': boolean;
  'ClientDetailView.newRule': string;
  'ClientDetailView.ruleType': 'block' | 'allow';
  'ClientDetailView.editingAlias': boolean;
  'ClientDetailView.aliasValue': string;
  'ClientDetailView.ruleError': string | null;
  'GroupDetailView.newRule': string;
  'GroupDetailView.ruleType': 'block' | 'allow';
  'GroupDetailView.newMemberIp': string;
  'GroupDetailView.confirmDelete': boolean;
  'GroupDetailView.ruleError': string | null;
  'RangeDetailView.newRule': string;
  'RangeDetailView.ruleType': 'block' | 'allow';
  'RangeDetailView.confirmDelete': boolean;
  'RangeDetailView.editing': boolean;
  'RangeDetailView.editName': string;
  'RangeDetailView.editCidr': string;
  'ListPane.showCreate': boolean;
  'ListPane.newName': string;
  'ListPane.newCidr': string;
  'ListPane.creating': boolean;
  'Rewrites.domain': string;
  'Rewrites.ip': string;
  'Rewrites.search': string;
  'Rewrites.editingId': number | null;
  'Rewrites.editDomain': string;
  'Rewrites.editIp': string;
  'Config.draft': {
    revision: number;
    changes: { [key: string]: { value: string; revision: number } };
  };
  'Config.saving': boolean;
  'Config.message': string;
  'UpstreamsCard.newUpstream': string;
  'BootstrapCard.newServer': string;
  'ClientSearch.clients': import('../api/generated').Client[];
  'ClientSearch.search': string;
  'ClientSearch.open': boolean;
  'Analysis.domains': string;
  'Analysis.clientIp': string;
  'Analysis.recordType': string;
  'Analysis.matrixResult': import('../api/generated').MatrixResponse | null;
  'Analysis.simResult': import('../api/generated').SimulateResponse | null;
  'Analysis.loading': boolean;
  'Analysis.loadingSource': string | null;
  'Analysis.sourceError': string | null;
  'Analysis.trafficWindow': string;
  'Analysis.hoveredCol': number | null;
  'useAdminController.nowMs': number | null;
  'useAdminController.showCreateToken': boolean;
  'useAdminController.newTokenName': string;
  'useAdminController.newTokenRole': string;
  'useAdminController.revealedToken': { name: string; token: string } | null;
  'useAdminController.currentUsername': string;
  'useAdminController.currentRole': string;
  'useAdminController.showCreateUser': boolean;
  'useAdminController.newUsername': string;
  'useAdminController.newPassword': string;
  'useAdminController.newUserRole': string;
  'useAdminController.editingUserId': number | null;
  'useAdminController.editRole': string;
  'useAdminController.editPassword': string;
  'useAdminController.confirmDeleteUser': number | null;
  'useAdminController.confirmRevokeToken': number | null;
  'useAdminController.showAddPeer': boolean;
  'useAdminController.newPeerUrl': string;
  'useAdminController.confirmRemovePeer': string | null;
  'useAdminController.editingInterval': boolean;
  'useAdminController.editIntervalValue': string;
  'useAdminController.editingSecret': boolean;
  'useAdminController.editSecretValue': string;
  'useAdminController.editingNodeName': boolean;
  'useAdminController.editNodeNameValue': string;
  'useAdminController.pairingMode': 'none' | 'initiate' | 'confirm';
  'useAdminController.pairingCode': string;
  'useAdminController.pairingSelfUrl': string;
  'useAdminController.confirmPeerUrl': string;
  'useAdminController.confirmCode': string;
  'useAdminController.pairingStatus': string;
  'Investigation.tables': import('../api/generated').InvestigationSchemaTable[];
  'Investigation.openTables': Set<string>;
  'Investigation.loading': boolean;
  'Investigation.result': import('../api/generated').InvestigationView | null;
  'Investigation.error': string | null;
  'Investigation.timeout': number;
  'Investigation.sql': string;
  'Investigation.editorHeight': number;
  'Login.username': string;
  'Login.password': string;
  'Login.error': string;
  'Login.loading': boolean;
  'useResource.tick': number;
  'Setup.model': import('./setupWizard').SetupState;
  'ConfigBackup.model': import('./configBackup').BackupState;
}
export type UiState = { [K in keyof UiFieldValues]: Record<string, UiFieldValues[K]> };
export const initialUiState: UiState = {
  'RequireAuth.state': {},
  'Sidebar.username': {},
  'Sidebar.authEnabled': {},
  'NodeNameProvider.nodeName': {},
  'Dashboard.refreshTick': {},
  'Dashboard.animateCharts': {},
  'Dashboard.observedLowerSections': {},
  'Logs.refreshInterval': {},
  'Logs.page': {},
  'Logs.logs': {},
  'Logs.total': {},
  'Logs.loading': {},
  'Logs.error': {},
  'Logs.expandedId': {},
  'Logs.evalDetail': {},
  'Logs.evalLoading': {},
  'Logs.contextMenu': {},
  'Filters.addUrl': {},
  'Filters.addName': {},
  'ListsTab.editingId': {},
  'ListsTab.editName': {},
  'HistoryTab.listFilter': {},
  'HistoryTab.search': {},
  'DetailsTab.selectedCheckpoint': {},
  'RecentActivityWidget.expandedId': {},
  'RecentActivityWidget.evalDetail': {},
  'RecentActivityWidget.evalLoading': {},
  'RecentActivityWidget.contextMenu': {},
  'ListToggleSection.collapsed': {},
  'PolicyDetailView.editName': {},
  'PolicyDetailView.editDesc': {},
  'PolicyDetailView.editing': {},
  'PolicyDetailView.newRule': {},
  'PolicyDetailView.ruleType': {},
  'PolicyDetailView.confirmDelete': {},
  'ClientDetailView.newRule': {},
  'ClientDetailView.ruleType': {},
  'ClientDetailView.editingAlias': {},
  'ClientDetailView.aliasValue': {},
  'ClientDetailView.ruleError': {},
  'GroupDetailView.newRule': {},
  'GroupDetailView.ruleType': {},
  'GroupDetailView.newMemberIp': {},
  'GroupDetailView.confirmDelete': {},
  'GroupDetailView.ruleError': {},
  'RangeDetailView.newRule': {},
  'RangeDetailView.ruleType': {},
  'RangeDetailView.confirmDelete': {},
  'RangeDetailView.editing': {},
  'RangeDetailView.editName': {},
  'RangeDetailView.editCidr': {},
  'ListPane.showCreate': {},
  'ListPane.newName': {},
  'ListPane.newCidr': {},
  'ListPane.creating': {},
  'Rewrites.domain': {},
  'Rewrites.ip': {},
  'Rewrites.search': {},
  'Rewrites.editingId': {},
  'Rewrites.editDomain': {},
  'Rewrites.editIp': {},
  'Config.draft': {},
  'Config.saving': {},
  'Config.message': {},
  'UpstreamsCard.newUpstream': {},
  'BootstrapCard.newServer': {},
  'ClientSearch.clients': {},
  'ClientSearch.search': {},
  'ClientSearch.open': {},
  'Analysis.domains': {},
  'Analysis.clientIp': {},
  'Analysis.recordType': {},
  'Analysis.matrixResult': {},
  'Analysis.simResult': {},
  'Analysis.loading': {},
  'Analysis.loadingSource': {},
  'Analysis.sourceError': {},
  'Analysis.trafficWindow': {},
  'Analysis.hoveredCol': {},
  'useAdminController.nowMs': {},
  'useAdminController.showCreateToken': {},
  'useAdminController.newTokenName': {},
  'useAdminController.newTokenRole': {},
  'useAdminController.revealedToken': {},
  'useAdminController.currentUsername': {},
  'useAdminController.currentRole': {},
  'useAdminController.showCreateUser': {},
  'useAdminController.newUsername': {},
  'useAdminController.newPassword': {},
  'useAdminController.newUserRole': {},
  'useAdminController.editingUserId': {},
  'useAdminController.editRole': {},
  'useAdminController.editPassword': {},
  'useAdminController.confirmDeleteUser': {},
  'useAdminController.confirmRevokeToken': {},
  'useAdminController.showAddPeer': {},
  'useAdminController.newPeerUrl': {},
  'useAdminController.confirmRemovePeer': {},
  'useAdminController.editingInterval': {},
  'useAdminController.editIntervalValue': {},
  'useAdminController.editingSecret': {},
  'useAdminController.editSecretValue': {},
  'useAdminController.editingNodeName': {},
  'useAdminController.editNodeNameValue': {},
  'useAdminController.pairingMode': {},
  'useAdminController.pairingCode': {},
  'useAdminController.pairingSelfUrl': {},
  'useAdminController.confirmPeerUrl': {},
  'useAdminController.confirmCode': {},
  'useAdminController.pairingStatus': {},
  'Investigation.tables': {},
  'Investigation.openTables': {},
  'Investigation.loading': {},
  'Investigation.result': {},
  'Investigation.error': {},
  'Investigation.timeout': {},
  'Investigation.sql': {},
  'Investigation.editorHeight': {},
  'Login.username': {},
  'Login.password': {},
  'Login.error': {},
  'Login.loading': {},
  'useResource.tick': {},
  'Setup.model': {},
  'ConfigBackup.model': {},
};

export type UiFieldUpdate<K extends keyof UiFieldValues> =
  | UiFieldValues[K]
  | ((previous: UiFieldValues[K]) => UiFieldValues[K]);
export function registerUiField<K extends keyof UiFieldValues>(
  ui: UiState,
  key: K,
  instance: string,
  initial: UiFieldValues[K],
): UiState {
  return instance in ui[key] ? ui : { ...ui, [key]: { ...ui[key], [instance]: initial } };
}
export function removeUiField(ui: UiState, key: keyof UiFieldValues, instance: string): UiState {
  const { [instance]: _removed, ...remaining } = ui[key];
  return { ...ui, [key]: remaining };
}
export function changeUiField<K extends keyof UiFieldValues>(
  ui: UiState,
  key: K,
  instance: string,
  update: UiFieldUpdate<K>,
): UiState {
  const current = ui[key][instance];
  if (current === undefined) return ui;
  const next = typeof update === 'function' ? update(current) : update;
  return Object.is(current, next) ? ui : { ...ui, [key]: { ...ui[key], [instance]: next } };
}
