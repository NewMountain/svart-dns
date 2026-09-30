import TopBar from '../components/TopBar';
import '../styles/pages/logs.css';
import '../styles/pages/tiers.css';
import { DetailPane } from './assignments/DetailPane';
import { ListPane } from './assignments/ListPane';
import { useTiersController } from './Tiers.controller';

export default function Tiers() {
  const model = useTiersController();
  return <TiersView model={model} />;
}

export { ClientDetailView } from './assignments/ClientDetail';

function TiersView({ model }: { model: NonNullable<ReturnType<typeof useTiersController>> }) {
  const {
    tab,
    selectedId,
    search,
    setTab,
    setSelectedId,
    setSearch,
    policies,
    refreshPolicies,
    ranges,
    refreshRanges,
    groups,
    refreshGroups,
    clients,
  } = model;
  return (
    <div className="tiers-page">
      <TopBar title="Assignments" />

      <div className="sub-nav">
        <button
          className={`tab-item${tab === 'ranges' ? ' active' : ''}`}
          onClick={() => {
            setTab('ranges');
          }}
        >
          Networks
        </button>
        <button
          className={`tab-item${tab === 'groups' ? ' active' : ''}`}
          onClick={() => {
            setTab('groups');
          }}
        >
          Groups
        </button>
        <button
          className={`tab-item${tab === 'clients' ? ' active' : ''}`}
          onClick={() => {
            setTab('clients');
          }}
        >
          Clients
        </button>
        <button
          className={`tab-item${tab === 'policies' ? ' active' : ''}`}
          onClick={() => {
            setTab('policies');
          }}
        >
          Bundles
        </button>
      </div>

      <div className="tiers-content">
        <div className="split-view">
          <ListPane
            tab={tab}
            search={search}
            setSearch={setSearch}
            selectedId={selectedId}
            setSelectedId={setSelectedId}
            policies={policies}
            ranges={ranges}
            groups={groups}
            clients={clients}
            refreshPolicies={refreshPolicies}
            refreshRanges={refreshRanges}
            refreshGroups={refreshGroups}
          />
          <DetailPane
            tab={tab}
            selectedId={selectedId}
            clients={clients}
            policies={policies}
            onDelete={() => {
              setSelectedId(null);
              refreshPolicies();
              refreshRanges();
              refreshGroups();
            }}
          />
        </div>
      </div>
    </div>
  );
}
