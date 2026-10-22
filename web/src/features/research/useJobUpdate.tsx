/** Cache refresh after a research job mutation. */
import { useQueryClient } from "@tanstack/react-query";
import type { Job } from "./jobStatus";

/**
 * Refresh research lists and the job's detail after the server returned or rejected a
 * versioned change.
 *
 * @returns Callback accepting the affected job.
 */
export function useJobUpdate(): (job: Job) => void {
  const queryClient = useQueryClient();
  return (job) => {
    void queryClient.invalidateQueries({ queryKey: ["research"] });
    void queryClient.invalidateQueries({ queryKey: ["research-detail", job.id] });
  };
}
