import type { PolicyView as PolicySummary } from '../../api/generated';
import {
  deleteApiClientsIpAllowDomainDomain,
  deleteApiClientsIpBlockDomainDomain,
  deleteApiClientsIpBlocklistsListId,
  deleteApiClientsIpPolicy,
  postApiClientsIpAllowDomain,
  postApiClientsIpBlockDomain,
  postApiClientsIpBlocklistsListId,
  postApiClientsIpPolicyPolicyId,
  putApiClientsIpAlias,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useApi } from '../../hooks/useApi';
import { useUiField } from '../../hooks/useUiField';
import { errorMessage } from './navigation';

export function useClientDetailViewController({
  ip,
  policies,
}: {
  ip: string;
  policies: PolicySummary[] | null;
}) {
  const action = useAction();
  const { data, refresh: reload } = useApi(
    `/api/clients/${encodeURIComponent(ip)}`,
    'getApiClientsIp',
  );
  const [newRule, setNewRule] = useUiField('ClientDetailView.newRule', '');
  const [ruleType, setRuleType] = useUiField('ClientDetailView.ruleType', 'block');
  const [editingAlias, setEditingAlias] = useUiField('ClientDetailView.editingAlias', false);
  const [aliasValue, setAliasValue] = useUiField('ClientDetailView.aliasValue', '');
  const [ruleError, setRuleError] = useUiField('ClientDetailView.ruleError', null);
  const { data: recentLogs } = useApi(
    '/api/query-logs',
    'getApiQueryLogs',
    { client_ip: ip, limit: '20' },
    [ip],
  );
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
        !currentlyAssigned ? postApiClientsIpBlocklistsListId : deleteApiClientsIpBlocklistsListId
      )(ip, blId);
      reload();
    })();
  }
  function addRule() {
    if (!newRule) return;
    const domain = newRule;
    const type = ruleType;
    action(async () => {
      await (type === 'block' ? postApiClientsIpBlockDomain : postApiClientsIpAllowDomain)(ip, {
        domain,
      });
      setNewRule('');
      reload();
    })();
  }
  function removeRule(domain: string, type: 'block' | 'allow') {
    setRuleError(null);
    void (
      type === 'block' ? deleteApiClientsIpBlockDomainDomain : deleteApiClientsIpAllowDomainDomain
    )(ip, domain)
      .then(reload)
      .catch((error: unknown) => {
        setRuleError('Unable to delete custom rule: ' + errorMessage(error));
      });
  }
  function assignPolicy(policyId: number) {
    action(async () => {
      await postApiClientsIpPolicyPolicyId(ip, policyId);
      reload();
    })();
  }
  function removePolicy() {
    action(async () => {
      await deleteApiClientsIpPolicy(ip);
      reload();
    })();
  }
  function saveAlias() {
    action(async () => {
      await putApiClientsIpAlias(ip, { alias: aliasValue.trim() });
      setEditingAlias(false);
      reload();
    })();
  }
  return {
    ip,
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    editingAlias,
    setEditingAlias,
    aliasValue,
    setAliasValue,
    ruleError,
    recentLogs,
    policyData,
    policyBlocklistIds,
    toggleBlocklist,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    saveAlias,
  };
}
