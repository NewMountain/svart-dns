import { useAppState } from '../hooks/appStateContext';

export default function ApplicationStatus() {
  const { state, dispatch } = useAppState();
  if (state.error === null) return null;
  return (
    <div role="alert" className="application-error">
      <span>{state.error}</span>
      <button
        className="btn btn-ghost"
        onClick={() => {
          dispatch({ type: 'dismissError' });
        }}
      >
        Dismiss error
      </button>
    </div>
  );
}
