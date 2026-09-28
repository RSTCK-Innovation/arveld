import { queryOptions, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNotify } from '../components/notifications';
import { createPreferencesStore, type Preferences } from './preferences';

// Access storage lazily: a browser may block localStorage even when it exists.
export const browserPreferences = createPreferencesStore({
  getItem: (key) => window.localStorage.getItem(key),
  setItem: (key, value) => window.localStorage.setItem(key, value),
});
export const preferencesOptions = queryOptions({
  queryKey: ['browser-preferences'],
  queryFn: () => browserPreferences.read(),
  staleTime: 10_000,
});
export function usePreferences() {
  return useQuery({
    ...preferencesOptions,
    refetchInterval: (query) => {
      const refresh = query.state.data?.refresh;
      return refresh && refresh !== 'off' ? Number(refresh) * 1000 : false;
    },
  });
}
export function useSavePreferences() {
  const client = useQueryClient();
  const notify = useNotify();
  return useMutation({
    mutationFn: async (input: Preferences) => browserPreferences.save(input),
    onSuccess: (data) => {
      client.setQueryData(preferencesOptions.queryKey, data);
      notify('Preferences saved');
    },
    onError: (error) => notify(error, 'error'),
  });
}
