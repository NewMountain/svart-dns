import type { Client, PolicyView as PolicySummary } from '../../api/generated';
import {
  deleteApiGroupsId,
  deleteApiGroupsIdAllowDomainDomain,
  deleteApiGroupsIdBlockDomainDomain,
  deleteApiGroupsIdBlocklistsListId,
  deleteApiGroupsIdMembersIp,
  deleteApiGroupsIdPolicy,
  postApiGroupsIdAllowDomain,
  postApiGroupsIdBlockDomain,
  postApiGroupsIdBlocklistsListId,
  postApiGroupsIdMembersIp,
  postApiGroupsIdPolicyPolicyId,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useApi } from '../../hooks/useApi';
import { useUiField } from '../../hooks/useUiField';
import { errorMessage } from './navigation';

export function useGroupDetailViewController({
  id,
  clients,
  policies,
  onDelete,
}: {
  id: string;
  clients: Client[] | null;
  policies: PolicySummary[] | null;
  onDelete: () => void;
}) {
  const action = useAction();
  const { data, refresh: reload } = useApi(
    `/api/groups/${encodeURIComponent(id)}`,
    'getApiGroupsId',
  );
  const [newRule, setNewRule] = useUiField('GroupDetailView.newRule', '');
  const [ruleType, setRuleType] = useUiField('GroupDetailView.ruleType', 'block');
  const [newMemberIp, setNewMemberIp] = useUiField('GroupDetailView.newMemberIp', '');
  const [confirmDelete, setConfirmDelete] = useUiField('GroupDetailView.confirmDelete', false);
  const [ruleError, setRuleError] = useUiField('GroupDetailView.ruleError', null);
  const policyId = data?.policy?.id;
  const { data: loadedPolicy } = useApi(
    policyId ? `/api/policies/${String(policyId)}` : null,
    'getApiPoliciesId',
  );
  const policyData = loadedPolicy?.id === policyId ? loadedPolicy : null;
  if (!data) return null;
  const memberIps = new Set((data.members ?? []).map((m) => m.ip_address));
  const availableClients = (clients ?? []).filter((c) => !memberIps.has(c.ip_address));
  const policyBlocklistIds = new Set(
    (policyData?.blocklists ?? []).filter((bl) => bl.is_assigned).map((bl) => bl.id),
  );
  function toggleBlocklist(blId: number, currentlyAssigned: boolean) {
    if (policyBlocklistIds.has(blId)) return;
    action(async () => {
      await (
        !currentlyAssigned ? postApiGroupsIdBlocklistsListId : deleteApiGroupsIdBlocklistsListId
      )(id, blId);
      reload();
    })();
  }
  function addMember() {
    if (!newMemberIp) return;
    const ip = newMemberIp;
    action(async () => {
      await postApiGroupsIdMembersIp(id, ip);
      setNewMemberIp('');
      reload();
    })();
  }
  function removeMember(ip: string) {
    action(async () => {
      await deleteApiGroupsIdMembersIp(id, ip);
      reload();
    })();
  }
  function addRule() {
    if (!newRule) return;
    const domain = newRule;
    const type = ruleType;
    action(async () => {
      await (type === 'block' ? postApiGroupsIdBlockDomain : postApiGroupsIdAllowDomain)(id, {
        domain,
      });
      setNewRule('');
      reload();
    })();
  }
  function removeRule(domain: string, type: 'block' | 'allow') {
    setRuleError(null);
    void (
      type === 'block' ? deleteApiGroupsIdBlockDomainDomain : deleteApiGroupsIdAllowDomainDomain
    )(id, domain)
      .then(reload)
      .catch((error: unknown) => {
        setRuleError('Unable to delete custom rule: ' + errorMessage(error));
      });
  }
  function assignPolicy(policyId: number) {
    action(async () => {
      await postApiGroupsIdPolicyPolicyId(id, policyId);
      reload();
    })();
  }
  function removePolicy() {
    action(async () => {
      await deleteApiGroupsIdPolicy(id);
      reload();
    })();
  }
  function handleDelete() {
    action(async () => {
      await deleteApiGroupsId(id);
      onDelete();
    })();
  }
  return {
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    newMemberIp,
    setNewMemberIp,
    confirmDelete,
    setConfirmDelete,
    ruleError,
    policyData,
    availableClients,
    policyBlocklistIds,
    toggleBlocklist,
    addMember,
    removeMember,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    handleDelete,
  };
}
