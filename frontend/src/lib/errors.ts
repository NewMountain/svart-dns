export function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'The operation failed. Please retry and check the server logs if it continues.';
}
