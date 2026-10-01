import * as RadixDialog from '@radix-ui/react-dialog'
import type { ReactNode } from 'react'

export const Dialog = RadixDialog.Root
export const DialogTitle = RadixDialog.Title
export const DialogDescription = RadixDialog.Description

export function DialogContent({ children }: { children: ReactNode }) {
  return (
    <RadixDialog.Portal>
      <RadixDialog.Overlay className="fixed inset-0 bg-black/60" />
      <RadixDialog.Content className="fixed top-1/2 left-1/2 max-h-[85vh] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-neutral-800 bg-neutral-900 p-6 shadow-xl focus:outline-none">
        {children}
      </RadixDialog.Content>
    </RadixDialog.Portal>
  )
}
