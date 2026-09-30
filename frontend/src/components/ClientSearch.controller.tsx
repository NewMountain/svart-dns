import { useEffect, useMemo, useRef } from 'react';
import { getApiClients } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';
interface Props {
  value: string;
  onChange: (ip: string) => void;
}
export function useClientSearchController({ value, onChange }: Props) {
  const action = useAction();
  const [clients, setClients] = useUiField('ClientSearch.clients', []);
  const [search, setSearch] = useUiField('ClientSearch.search', '');
  const [open, setOpen] = useUiField('ClientSearch.open', false);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    action(() =>
      getApiClients().then((clients) => {
        setClients(clients ?? []);
      }),
    )();
  }, [action, setClients]);
  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', onMouseDown);
    return () => {
      document.removeEventListener('mousedown', onMouseDown);
    };
  }, [action, setOpen]);
  const filtered = useMemo(() => {
    if (!search.trim()) return clients;
    const q = search.toLowerCase();
    return clients.filter(
      (c) =>
        c.ip_address.toLowerCase().includes(q) || (c.alias && c.alias.toLowerCase().includes(q)),
    );
  }, [clients, search]);
  const selectedAlias = useMemo(
    () => (value ? clients.find((c) => c.ip_address === value)?.alias : null),
    [clients, value],
  );
  return {
    value,
    onChange,
    clients,
    search,
    setSearch,
    open,
    setOpen,
    containerRef,
    inputRef,
    filtered,
    selectedAlias,
  };
}
