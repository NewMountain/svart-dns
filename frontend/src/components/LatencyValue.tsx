interface LatencyValueProps {
  microseconds: number;
}

export default function LatencyValue({ microseconds }: LatencyValueProps) {
  if (!Number.isFinite(microseconds) || microseconds < 0) {
    return <span title="The recorded latency sample is invalid.">Unavailable</span>;
  }
  if (microseconds === 0) {
    return (
      <span title="No clock tick elapsed. DNS timing uses an approximately 500µs sampled clock; this is not an exact zero.">
        ≈0ms
      </span>
    );
  }
  return (
    <>
      {microseconds > 1000 ? `${(microseconds / 1000).toFixed(0)}ms` : `${String(microseconds)}µs`}
    </>
  );
}
