import type { PolicyView as PolicySummary } from '../../api/generated';
import {
  deleteApiRangesId,
  deleteApiRangesIdAllowDomainDomain,
  deleteApiRangesIdBlockDomainDomain,
  deleteApiRangesIdBlocklistsListId,
  deleteApiRangesIdPolicy,
  postApiRangesIdAllowDomain,
  postApiRangesIdBlockDomain,
  postApiRangesIdBlocklistsListId,
  postApiRangesIdPolicyPolicyId,
  putApiRangesId,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useApi } from '../../hooks/useApi';
import { useUiField } from '../../hooks/useUiField';

export function useRangeDetailViewController({
  id,
  policies,
  onDelete,
}: {
  id: string;
  policies: PolicySummary[] | null;
  onDelete: () => void;
}) {
  const action = useAction();
  const { data, refresh: reload } = useApi(
    `/api/ranges/${encodeURIComponent(id)}`,
    'getApiRangesId',
  );
  const [newRule, setNewRule] = useUiField('RangeDetailView.newRule', '');
  const [ruleType, setRuleType] = useUiField('RangeDetailView.ruleType', 'block');
  const [confirmDelete, setConfirmDelete] = useUiField('RangeDetailView.confirmDelete', false);
  const [editing, setEditing] = useUiField('RangeDetailView.editing', false);
  const [editName, setEditName] = useUiField('RangeDetailView.editName', '');
  const [editCidr, setEditCidr] = useUiField('RangeDetailView.editCidr', '');
  const policyId = data?.policy?.id;
  const { data: loadedPolicy } = useApi(
    policyId ? `/api/policies/${String(policyId)}` : null,
    'getApiPoliciesId',
  );
  const policyData = loadedPolicy?.id === policyId ? loadedPolicy : null;
  if (!data) return null;
  const policyBlocklistIds = new Set(
    (policyData?.blocklists ?? []).filter((bl) => bl.is_assigned).map((bl) => bl.id),
  );
  function toggleBlocklist(blId: number, currentlyAssigned: boolean) {
    if (policyBlocklistIds.has(blId)) return;
    action(async () => {
      await (
        !currentlyAssigned ? postApiRangesIdBlocklistsListId : deleteApiRangesIdBlocklistsListId
      )(id, blId);
      reload();
    })();
  }
  function addRule() {
    if (!newRule) return;
    const domain = newRule;
    const type = ruleType;
    action(async () => {
      await (type === 'block' ? postApiRangesIdBlockDomain : postApiRangesIdAllowDomain)(id, {
        domain,
      });
      setNewRule('');
      reload();
    })();
  }
  function removeRule(domain: string, type: 'block' | 'allow') {
    action(async () => {
      await (
        type === 'block' ? deleteApiRangesIdBlockDomainDomain : deleteApiRangesIdAllowDomainDomain
      )(id, domain);
      reload();
    })();
  }
  function assignPolicy(policyId: number) {
    action(async () => {
      await postApiRangesIdPolicyPolicyId(id, policyId);
      reload();
    })();
  }
  function removePolicy() {
    action(async () => {
      await deleteApiRangesIdPolicy(id);
      reload();
    })();
  }
  function handleDelete() {
    action(async () => {
      await deleteApiRangesId(id);
      onDelete();
    })();
  }
  function startEditing() {
    if (!data) return;
    setEditName(data.name);
    setEditCidr(data.cidr);
    setEditing(true);
  }
  function saveRange() {
    action(async () => {
      await putApiRangesId(id, { name: editName.trim(), cidr: editCidr.trim() });
      setEditing(false);
      reload();
    })();
  }
  return {
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    confirmDelete,
    setConfirmDelete,
    editing,
    setEditing,
    editName,
    setEditName,
    editCidr,
    setEditCidr,
    policyData,
    policyBlocklistIds,
    toggleBlocklist,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    handleDelete,
    startEditing,
    saveRange,
  };
}
