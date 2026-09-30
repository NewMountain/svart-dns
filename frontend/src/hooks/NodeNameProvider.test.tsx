import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import * as apiClient from '../api/operations';
import { AppStateProvider } from './AppStateProvider';
import { NodeNameProvider } from './NodeNameProvider';
import { useNodeName } from './useNodeName';

vi.mock('../api/operations', () => ({
  getApiPeers: vi.fn(),
}));

const getMock = vi.mocked(apiClient.getApiPeers);

function NodeNameConsumer() {
  const { nodeName } = useNodeName();
  return <span>{nodeName}</span>;
}

describe('NodeNameProvider', () => {
  beforeEach(() => {
    getMock.mockReset();
  });

  it('loads the display name from the authenticated peers endpoint', async () => {
    getMock.mockResolvedValue({
      node_name: 'svart-a',
      node_id: 'fixture-node',
      peers: [],
      sync_interval: '5s',
      has_secret: false,
      tls_configured: false,
      replicate_identity: false,
    });

    render(
      <AppStateProvider>
        {
          <NodeNameProvider>
            <NodeNameConsumer />
          </NodeNameProvider>
        }
      </AppStateProvider>,
    );

    expect(await screen.findByText('svart-a')).toBeInTheDocument();
    await waitFor(() => {
      expect(getMock).toHaveBeenCalledWith();
    });
  });
});
