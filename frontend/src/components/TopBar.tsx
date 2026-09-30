import { ReactNode } from 'react';
import { useNodeName } from '../hooks/useNodeName';

interface TopBarProps {
  title: string;
  children?: ReactNode;
}

export default function TopBar({ title, children }: TopBarProps) {
  const { nodeName } = useNodeName();

  return (
    <div className="top-bar">
      <div style={{ display: 'flex', gap: '16px', alignItems: 'center' }}>
        <div className="text-lg">{title}</div>
        {children}
      </div>
      {nodeName && (
        <div className="instance-selector">
          <span style={{ color: 'var(--green)' }}>●</span> {nodeName}
        </div>
      )}
    </div>
  );
}
