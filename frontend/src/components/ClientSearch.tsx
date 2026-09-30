import { useClientSearchController } from './ClientSearch.controller';

interface Props {
  value: string;
  onChange: (ip: string) => void;
}

export default function ClientSearch({ value, onChange }: Props) {
  const model = useClientSearchController({ value, onChange });
  return <ClientSearchView model={model} />;
}

function ClientSearchView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useClientSearchController>>;
}) {
  const {
    value,
    onChange,
    search,
    setSearch,
    open,
    setOpen,
    containerRef,
    inputRef,
    filtered,
    selectedAlias,
  } = model;
  return (
    <div className="client-search" ref={containerRef}>
      <input
        ref={inputRef}
        type="text"
        className="analysis-input-field"
        placeholder="Search clients..."
        value={open ? search : value}
        onFocus={() => {
          setSearch('');
          setOpen(true);
        }}
        onChange={(e) => {
          setSearch(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false);
          const onlyClient = filtered.length === 1 ? filtered[0] : undefined;
          if (e.key === 'Enter' && onlyClient) {
            onChange(onlyClient.ip_address);
            setSearch('');
            setOpen(false);
          }
        }}
        style={{ width: '100%' }}
      />
      {open && filtered.length > 0 && (
        <div className="client-search-dropdown">
          {filtered.map((c) => (
            <div
              key={c.ip_address}
              className="client-search-item"
              onMouseDown={(e) => {
                e.preventDefault();
              }}
              onClick={() => {
                onChange(c.ip_address);
                setSearch('');
                setOpen(false);
              }}
            >
              {c.alias ? `${c.alias} (${c.ip_address})` : c.ip_address}
            </div>
          ))}
        </div>
      )}
      {!open && value && selectedAlias && (
        <div className="client-search-alias">{selectedAlias}</div>
      )}
    </div>
  );
}
