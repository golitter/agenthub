import { cn } from '@/lib/utils'

import { Dialog, DialogContent } from './dialog'

type SheetContentProps = React.ComponentProps<typeof DialogContent> & {
  side?: 'top' | 'right' | 'bottom' | 'left'
}

function Sheet(props: React.ComponentProps<typeof Dialog>) {
  return <Dialog {...props} />
}

function SheetContent({ side = 'right', className, ...props }: SheetContentProps) {
  return (
    <DialogContent
      {...props}
      className={cn(
        '!translate-x-0 !translate-y-0 !gap-0 !rounded-none !border-y-0 !p-0 !max-w-none sm:!p-0',
        (side === 'left' || side === 'right') &&
          '!bottom-auto !top-0 !h-full !max-h-[100dvh] !w-[min(90vw,22rem)]',
        side === 'left' && '!left-0 !right-auto',
        side === 'right' && '!left-auto !right-0',
        side === 'top' && '!bottom-auto !left-0 !right-auto !top-0 !h-auto !w-full',
        side === 'bottom' && '!bottom-0 !left-0 !right-auto !top-auto !h-auto !w-full',
        className,
      )}
    />
  )
}

export { Sheet, SheetContent }
