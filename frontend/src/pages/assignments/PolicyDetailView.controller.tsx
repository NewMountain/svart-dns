import {
  deleteApiPoliciesId,
  deleteApiPoliciesIdAllowDomainDomain,
  deleteApiPoliciesIdAllowlistsListId,
  deleteApiPoliciesIdBlockDomainDomain,
  deleteApiPoliciesIdBlocklistsListId,
  postApiPoliciesIdAllowDomain,
  postApiPoliciesIdAllowlistsListId,
  postApiPoliciesIdBlockDomain,
  postApiPoliciesIdBlocklistsListId,
  putApiPoliciesId,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useApi } from '../../hooks/useApi';
import { useUiField } from '../../hooks/useUiField';

export function usePolicyDetailViewController({
  id,
  onDelete,
}: {
  id: string;
  onDelete: () => void;
}) {
  const action = useAction();
  const { data, refresh: reload } = useApi(
    `/api/policies/${encodeURIComponent(id)}`,
    'getApiPoliciesId',
  );
  const [editName, setEditName] = useUiField('PolicyDetailView.editName', '');
  const [editDesc, setEditDesc] = useUiField('PolicyDetailView.editDesc', '');
  const [editing, setEditing] = useUiField('PolicyDetailView.editing', false);
  const [newRule, setNewRule] = useUiField('PolicyDetailView.newRule', '');
  const [ruleType, setRuleType] = useUiField('PolicyDetailView.ruleType', 'block');
  const [confirmDelete, setConfirmDelete] = useUiField('PolicyDetailView.confirmDelete', false);
  if (!data) return null;
  function saveMeta() {
    action(async () => {
      await putApiPoliciesId(id, { name: editName.trim(), description: editDesc.trim() });
      setEditing(false);
      reload();
    })();
  }
  function toggleBlocklist(blId: number, currentlyAssigned: boolean) {
    action(async () => {
      await (
        !currentlyAssigned ? postApiPoliciesIdBlocklistsListId : deleteApiPoliciesIdBlocklistsListId
      )(id, blId);
      reload();
    })();
  }
  function toggleAllowlist(alId: number, currentlyAssigned: boolean) {
    action(async () => {
      await (
        !currentlyAssigned ? postApiPoliciesIdAllowlistsListId : deleteApiPoliciesIdAllowlistsListId
      )(id, alId);
      reload();
    })();
  }
  function addRule() {
    if (!newRule) return;
    const domain = newRule;
    const type = ruleType;
    action(async () => {
      await (type === 'block' ? postApiPoliciesIdBlockDomain : postApiPoliciesIdAllowDomain)(id, {
        domain,
      });
      setNewRule('');
      reload();
    })();
  }
  function removeRule(domain: string, type: 'block' | 'allow') {
    action(async () => {
      await (
        type === 'block'
          ? deleteApiPoliciesIdBlockDomainDomain
          : deleteApiPoliciesIdAllowDomainDomain
      )(id, domain);
      reload();
    })();
  }
  const { ranges, groups, clients } = data.assigned_to;
  const totalEntities = (ranges ?? []).length + (groups ?? []).length + (clients ?? []).length;
  function handleDelete() {
    action(async () => {
      await deleteApiPoliciesId(id);
      onDelete();
    })();
  }
  return {
    id,
    data,
    editName,
    setEditName,
    editDesc,
    setEditDesc,
    editing,
    setEditing,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    confirmDelete,
    setConfirmDelete,
    saveMeta,
    toggleBlocklist,
    toggleAllowlist,
    addRule,
    removeRule,
    ranges,
    groups,
    clients,
    totalEntities,
    handleDelete,
  };
}
