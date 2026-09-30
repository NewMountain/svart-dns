import TopBar from '../components/TopBar';
import '../styles/pages/dashboard.css';
import '../styles/pages/logs.css';
import { useDashboardController } from './Dashboard.controller';
import { validWindows } from './Dashboard.shared';
import { DistributionRow } from './dashboard/DistributionRow';
import { LiveQueryLogCard } from './dashboard/LiveQueryLogCard';
import { OverridesFailuresRow } from './dashboard/OverridesFailuresRow';
import { SummaryCards } from './dashboard/SummaryCards';
import { SystemResourcesRow } from './dashboard/SystemResourcesRow';
import { TopClientActivityCard } from './dashboard/TopClientActivityCard';
import { TopDomainsRow } from './dashboard/TopDomainsRow';
import { TrafficLatencyRow } from './dashboard/TrafficLatencyRow';

export default function Dashboard() {
  const model = useDashboardController();
  return <DashboardView model={model} />;
}

function DashboardView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useDashboardController>>;
}) {
  const {
    timeWindow,
    animateCharts,
    showLowerSections,
    setLowerSectionsGateNode,
    summary,
    summaryStatus,
    timeseriesStatus,
    latencyStatus,
    topClientsStatus,
    blockSourcesStatus,
    upstreamStatus,
    topPermitted,
    topPermittedStatus,
    topBlocked,
    topBlockedStatus,
    servfailsStatus,
    systemStatus,
    timeseries,
    latency,
    topClients,
    blockSources,
    upstreamUsage,
    servfails,
    system,
    setTimeWindow,
  } = model;
  return (
    <>
      <TopBar title="Dashboard">
        <div className="time-control">
          {validWindows.map((windowValue) => (
            <button
              key={windowValue}
              className={`time-btn${windowValue === timeWindow ? ' active' : ''}`}
              onClick={() => {
                setTimeWindow(windowValue);
              }}
            >
              {windowValue}
            </button>
          ))}
        </div>
      </TopBar>

      <div className="dashboard-container">
        <SummaryCards summary={summary} status={summaryStatus} />
        <TrafficLatencyRow
          timeseries={timeseries}
          latency={latency}
          avgLatencyMicroseconds={summary?.avg_latency_microseconds ?? null}
          timeseriesStatus={timeseriesStatus}
          latencyStatus={latencyStatus}
          animateCharts={animateCharts}
        />
        <DistributionRow
          summary={summary}
          blockSources={blockSources}
          upstreamUsage={upstreamUsage}
          summaryStatus={summaryStatus}
          blockSourcesStatus={blockSourcesStatus}
          upstreamStatus={upstreamStatus}
          animateCharts={animateCharts}
        />
        <div ref={setLowerSectionsGateNode} className="dashboard-section-gate" />
        <TopClientActivityCard
          topClients={topClients}
          status={topClientsStatus}
          animateCharts={animateCharts}
        />
        <TopDomainsRow
          topPermitted={topPermitted}
          topBlocked={topBlocked}
          permittedStatus={topPermittedStatus}
          blockedStatus={topBlockedStatus}
          animateCharts={animateCharts}
        />
        <OverridesFailuresRow
          summary={summary}
          servfails={servfails}
          summaryStatus={summaryStatus}
          servfailsStatus={servfailsStatus}
          animateCharts={animateCharts}
        />
        <SystemResourcesRow system={system} status={systemStatus} animateCharts={animateCharts} />
        <LiveQueryLogCard enabled={showLowerSections} />
      </div>
    </>
  );
}
