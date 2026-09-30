import { useEffect } from 'react';
import { relativeTime } from '../../lib/relativeTime';
import {
  deleteApiPeersUrl,
  deleteApiTokensId,
  deleteApiUsersId,
  getApiAuthCheck,
  postApiPeers,
  postApiPeersConfirm,
  postApiPeersPair,
  postApiTokens,
  postApiUsers,
  putApiSettingsKey,
  putApiUsersId,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useApi, usePolling } from '../../hooks/useApi';
import { useNodeName } from '../../hooks/useNodeName';
import { useUiField } from '../../hooks/useUiField';
import '../../styles/pages/admin.css';
export function useAdminController() {
  const action = useAction();
  const [nowMs, setNowMs] = useUiField('useAdminController.nowMs', null);
  useEffect(() => {
    const updateClock = () => {
      setNowMs(Date.now());
    };
    updateClock();
    const timer = setInterval(updateClock, 60000);
    return () => {
      clearInterval(timer);
    };
  }, [setNowMs]);
  const { data: tokens, refresh: refreshTokens } = useApi('/api/tokens', 'getApiTokens');
  const { data: users, refresh: refreshUsers } = useApi('/api/users', 'getApiUsers');
  const [showCreateToken, setShowCreateToken] = useUiField(
    'useAdminController.showCreateToken',
    false,
  );
  const [newTokenName, setNewTokenName] = useUiField('useAdminController.newTokenName', '');
  const [newTokenRole, setNewTokenRole] = useUiField('useAdminController.newTokenRole', 'readonly');
  const [revealedToken, setRevealedToken] = useUiField('useAdminController.revealedToken', null);
  const [currentUsername, setCurrentUsername] = useUiField(
    'useAdminController.currentUsername',
    '',
  );
  const [currentRole, setCurrentRole] = useUiField('useAdminController.currentRole', '');
  const [showCreateUser, setShowCreateUser] = useUiField(
    'useAdminController.showCreateUser',
    false,
  );
  const [newUsername, setNewUsername] = useUiField('useAdminController.newUsername', '');
  const [newPassword, setNewPassword] = useUiField('useAdminController.newPassword', '');
  const [newUserRole, setNewUserRole] = useUiField('useAdminController.newUserRole', 'admin');
  const [editingUserId, setEditingUserId] = useUiField('useAdminController.editingUserId', null);
  const [editRole, setEditRole] = useUiField('useAdminController.editRole', '');
  const [editPassword, setEditPassword] = useUiField('useAdminController.editPassword', '');
  const [confirmDeleteUser, setConfirmDeleteUser] = useUiField(
    'useAdminController.confirmDeleteUser',
    null,
  );
  const [confirmRevokeToken, setConfirmRevokeToken] = useUiField(
    'useAdminController.confirmRevokeToken',
    null,
  );
  const { setNodeName } = useNodeName();
  const { data: syncConfig, refresh: refreshSync } = usePolling('/api/peers', 'getApiPeers', 5000);
  const [showAddPeer, setShowAddPeer] = useUiField('useAdminController.showAddPeer', false);
  const [newPeerUrl, setNewPeerUrl] = useUiField('useAdminController.newPeerUrl', '');
  const [confirmRemovePeer, setConfirmRemovePeer] = useUiField(
    'useAdminController.confirmRemovePeer',
    null,
  );
  const [editingInterval, setEditingInterval] = useUiField(
    'useAdminController.editingInterval',
    false,
  );
  const [editIntervalValue, setEditIntervalValue] = useUiField(
    'useAdminController.editIntervalValue',
    '',
  );
  const [editingSecret, setEditingSecret] = useUiField('useAdminController.editingSecret', false);
  const [editSecretValue, setEditSecretValue] = useUiField(
    'useAdminController.editSecretValue',
    '',
  );
  const [editingNodeName, setEditingNodeName] = useUiField(
    'useAdminController.editingNodeName',
    false,
  );
  const [editNodeNameValue, setEditNodeNameValue] = useUiField(
    'useAdminController.editNodeNameValue',
    '',
  );
  const [pairingMode, setPairingMode] = useUiField('useAdminController.pairingMode', 'none');
  const [pairingCode, setPairingCode] = useUiField('useAdminController.pairingCode', '');
  const [pairingSelfUrl, setPairingSelfUrl] = useUiField('useAdminController.pairingSelfUrl', '');
  const [confirmPeerUrl, setConfirmPeerUrl] = useUiField('useAdminController.confirmPeerUrl', '');
  const [confirmCode, setConfirmCode] = useUiField('useAdminController.confirmCode', '');
  const [pairingStatus, setPairingStatus] = useUiField('useAdminController.pairingStatus', '');
  useEffect(() => {
    action(() =>
      getApiAuthCheck().then((data) => {
        setCurrentUsername(data.username || '');
        setCurrentRole(data.role || '');
      }),
    )();
  }, [action, setCurrentRole, setCurrentUsername]);
  const isAdmin = currentRole === 'admin';
  async function createToken() {
    if (!newTokenName) return;
    const data = await postApiTokens({ name: newTokenName, role: newTokenRole });
    setRevealedToken({ name: data.name, token: data.token });
    setShowCreateToken(false);
    setNewTokenName('');
    setNewTokenRole('readonly');
    refreshTokens();
  }
  async function revokeToken(id: number) {
    await deleteApiTokensId(id);
    setConfirmRevokeToken(null);
    refreshTokens();
  }
  function copyToken() {
    if (revealedToken) {
      action(() => navigator.clipboard.writeText(revealedToken.token))();
    }
  }
  async function createUser() {
    if (!newUsername || !newPassword) return;
    await postApiUsers({ username: newUsername, password: newPassword, role: newUserRole });
    setShowCreateUser(false);
    setNewUsername('');
    setNewPassword('');
    setNewUserRole('admin');
    refreshUsers();
  }
  async function updateUser(id: number) {
    const body: { role?: string; password?: string } = {};
    if (editRole) body.role = editRole;
    if (editPassword) body.password = editPassword;
    await putApiUsersId(id, body);
    setEditingUserId(null);
    setEditRole('');
    setEditPassword('');
    refreshUsers();
  }
  async function deleteUser(id: number) {
    await deleteApiUsersId(id);
    setConfirmDeleteUser(null);
    refreshUsers();
  }
  function formatDate(dateStr: string) {
    if (!dateStr) return '-';
    const d = new Date(dateStr);
    return d.toLocaleDateString('en-US', { month: 'short', day: '2-digit', year: 'numeric' });
  }
  function formatLastUsed(dateStr?: string) {
    return relativeTime(nowMs, dateStr);
  }
  async function addPeer() {
    if (!newPeerUrl) return;
    await postApiPeers({ url: newPeerUrl });
    setShowAddPeer(false);
    setNewPeerUrl('');
    refreshSync();
  }
  async function initiatePairing() {
    const result = await postApiPeersPair();
    setPairingCode(result.pairing_code);
    setPairingSelfUrl(result.self_url);
    setPairingMode('initiate');
    setPairingStatus('');
  }
  async function confirmPairing() {
    if (!confirmPeerUrl || !confirmCode) return;
    setPairingStatus('Connecting to peer...');
    try {
      await postApiPeersConfirm({
        peer_url: confirmPeerUrl,
        pairing_code: confirmCode,
      });
      setPairingStatus('Pairing successful!');
      setPairingMode('none');
      setConfirmPeerUrl('');
      setConfirmCode('');
      refreshSync();
    } catch (e) {
      setPairingStatus(`Pairing failed: ${e instanceof Error ? e.message : 'Unknown error'}`);
    }
  }
  async function removePeer(peerUrl: string) {
    await deleteApiPeersUrl(peerUrl);
    setConfirmRemovePeer(null);
    refreshSync();
  }
  async function saveInterval() {
    if (!editIntervalValue) return;
    await putApiSettingsKey('sync_interval', { value: editIntervalValue });
    setEditingInterval(false);
    setEditIntervalValue('');
    refreshSync();
  }
  async function saveSecret() {
    const secret = editSecretValue.trim();
    if (secret.length < 32) return;
    await putApiSettingsKey('sync_secret', { value: secret });
    setEditingSecret(false);
    setEditSecretValue('');
    refreshSync();
  }
  async function saveNodeName() {
    const name = editNodeNameValue.trim();
    if (!name) return;
    await putApiSettingsKey('node_name', { value: name });
    setNodeName(name);
    setEditingNodeName(false);
    setEditNodeNameValue('');
    refreshSync();
  }
  return {
    action,
    tokens,
    users,
    showCreateToken,
    setShowCreateToken,
    newTokenName,
    setNewTokenName,
    newTokenRole,
    setNewTokenRole,
    revealedToken,
    setRevealedToken,
    currentUsername,
    showCreateUser,
    setShowCreateUser,
    newUsername,
    setNewUsername,
    newPassword,
    setNewPassword,
    newUserRole,
    setNewUserRole,
    editingUserId,
    setEditingUserId,
    editRole,
    setEditRole,
    editPassword,
    setEditPassword,
    confirmDeleteUser,
    setConfirmDeleteUser,
    confirmRevokeToken,
    setConfirmRevokeToken,
    syncConfig,
    showAddPeer,
    setShowAddPeer,
    newPeerUrl,
    setNewPeerUrl,
    confirmRemovePeer,
    setConfirmRemovePeer,
    editingInterval,
    setEditingInterval,
    editIntervalValue,
    setEditIntervalValue,
    editingSecret,
    setEditingSecret,
    editSecretValue,
    setEditSecretValue,
    editingNodeName,
    setEditingNodeName,
    editNodeNameValue,
    setEditNodeNameValue,
    pairingMode,
    setPairingMode,
    pairingCode,
    setPairingCode,
    pairingSelfUrl,
    confirmPeerUrl,
    setConfirmPeerUrl,
    confirmCode,
    setConfirmCode,
    pairingStatus,
    setPairingStatus,
    isAdmin,
    createToken,
    revokeToken,
    copyToken,
    createUser,
    updateUser,
    deleteUser,
    formatDate,
    formatLastUsed,
    addPeer,
    initiatePairing,
    confirmPairing,
    removePeer,
    saveInterval,
    saveSecret,
    saveNodeName,
  };
}
export type AdminController = ReturnType<typeof useAdminController>;
