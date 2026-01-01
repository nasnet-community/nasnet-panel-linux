import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { listXrayVersions, uploadXrayBinary, deleteXrayVersion, triggerXrayDownload } from "../api/xray"

const KEY = ["xray-versions"]

export function useXrayVersions() {
  return useQuery({ queryKey: KEY, queryFn: listXrayVersions })
}

export function useUploadXrayBinary() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: { version: string; file: File }) => uploadXrayBinary(args.version, args.file),
    onSuccess: (res) => {
      toast.success(`Stored ${res.version} (${res.arch})`)
      void qc.invalidateQueries({ queryKey: KEY })
    },
    onError: (e: Error) => toast.error(e.message),
  })
}

export function useDeleteXrayVersion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (version: string) => deleteXrayVersion(version),
    onSuccess: () => {
      toast.success("Version deleted")
      void qc.invalidateQueries({ queryKey: KEY })
    },
    onError: (e: Error) => toast.error(e.message),
  })
}

export function useTriggerXrayDownload() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (version: string) => triggerXrayDownload(version),
    onSuccess: () => {
      toast.success("Download started")
      setTimeout(() => void qc.invalidateQueries({ queryKey: KEY }), 3000)
    },
    onError: (e: Error) => toast.error(e.message),
  })
}
