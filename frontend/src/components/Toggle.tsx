interface ToggleProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  size?: 'sm' | 'lg';
}

export default function Toggle({ checked, onChange, disabled, size }: ToggleProps) {
  return (
    <label className={`switch${size === 'lg' ? ' switch-lg' : ''}`}>
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => {
          onChange(e.target.checked);
        }}
        disabled={disabled}
      />
      <span className="slider" />
    </label>
  );
}
