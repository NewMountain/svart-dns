interface StatCardProps {
  label: string;
  value: string | number;
  valueColor?: string;
  trend?: string;
  trendColor?: string;
  accentTop?: string;
}

export default function StatCard({
  label,
  value,
  valueColor,
  trend,
  trendColor,
  accentTop,
}: StatCardProps) {
  return (
    <div
      className="stat-card"
      style={accentTop ? { borderTop: `2px solid var(--${accentTop})` } : undefined}
    >
      <span className="stat-label">{label}</span>
      <span className={`stat-value${valueColor ? ` text-${valueColor}` : ''}`}>{value}</span>
      {trend && (
        <span className={`stat-trend${trendColor ? ` text-${trendColor}` : ' text-muted'}`}>
          {trend}
        </span>
      )}
    </div>
  );
}
