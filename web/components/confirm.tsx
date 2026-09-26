"use client"

import * as React from "react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { buttonVariants } from "@/components/ui/button"
import { useI18n } from "@/lib/i18n"

export interface ConfirmRequest {
  title: string
  description: string
  action: string
  destructive?: boolean
  onConfirm: () => Promise<void> | void
}

/** Imperative confirmation dialog: `const [confirm, dialog] = useConfirm()`. */
export function useConfirm(): [(r: ConfirmRequest) => void, React.ReactNode] {
  const { t } = useI18n()
  const [req, setReq] = React.useState<ConfirmRequest | null>(null)
  const [pending, setPending] = React.useState(false)

  const dialog = (
    <AlertDialog open={!!req} onOpenChange={(o) => !o && !pending && setReq(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{req?.title}</AlertDialogTitle>
          <AlertDialogDescription>{req?.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            disabled={pending}
            className={req?.destructive ? buttonVariants({ variant: "destructive" }) : undefined}
            onClick={async (e) => {
              e.preventDefault()
              if (!req) return
              setPending(true)
              try {
                await req.onConfirm()
                setReq(null)
              } finally {
                setPending(false)
              }
            }}
          >
            {req?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
  return [setReq, dialog]
}
