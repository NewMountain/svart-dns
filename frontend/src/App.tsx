import { Outlet } from 'react-router-dom';
import RequireAuth from './components/RequireAuth';
import Sidebar from './components/Sidebar';
import { NodeNameProvider } from './hooks/NodeNameProvider';
import './styles/components.css';
import './styles/global.css';
import './styles/sidebar.css';
import './styles/variables.css';

export default function App() {
  return (
    <RequireAuth>
      <NodeNameProvider>
        <Sidebar />
        <div className="main-content">
          <Outlet />
        </div>
      </NodeNameProvider>
    </RequireAuth>
  );
}
