import * as React from "react"
import * as TabsPrimitive from "@radix-ui/react-tabs"
import { cn } from "@/lib/utils"

const Tabs = TabsPrimitive.Root

/** "default" is the boxed segmented look; "line" is page-level navigation:
 *  a hairline rule spanning the container with an underline on the active tab. */
type TabsVariant = "default" | "line"

const TabsVariantContext = React.createContext<TabsVariant>("default")

const listStyles: Record<TabsVariant, string> = {
    default:
        "inline-flex h-10 items-center justify-center rounded-lg bg-muted p-1 text-muted-foreground",
    line: "flex w-full items-center justify-start gap-6 overflow-x-auto shadow-[inset_0_-1px_0_0_var(--border)]",
}

const triggerStyles: Record<TabsVariant, string> = {
    default:
        "inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium ring-offset-background transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm",
    line: "group relative inline-flex items-center gap-2 whitespace-nowrap rounded-sm px-0 py-2.5 text-sm font-medium text-text-tertiary transition-colors hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-[state=active]:text-text-primary after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:rounded-full after:bg-transparent after:transition-colors hover:data-[state=inactive]:after:bg-border-strong data-[state=active]:after:bg-primary",
}

const TabsList = React.forwardRef<
    React.ElementRef<typeof TabsPrimitive.List>,
    React.ComponentPropsWithoutRef<typeof TabsPrimitive.List> & { variant?: TabsVariant }
>(({ className, variant = "default", ...props }, ref) => (
    <TabsVariantContext.Provider value={variant}>
        <TabsPrimitive.List
            ref={ref}
            className={cn(listStyles[variant], className)}
            {...props}
        />
    </TabsVariantContext.Provider>
))
TabsList.displayName = TabsPrimitive.List.displayName

const TabsTrigger = React.forwardRef<
    React.ElementRef<typeof TabsPrimitive.Trigger>,
    React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => {
    const variant = React.useContext(TabsVariantContext)
    return (
        <TabsPrimitive.Trigger
            ref={ref}
            className={cn(triggerStyles[variant], className)}
            {...props}
        />
    )
})
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName

const TabsContent = React.forwardRef<
    React.ElementRef<typeof TabsPrimitive.Content>,
    React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
    <TabsPrimitive.Content
        ref={ref}
        className={cn(
            "mt-2 ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
            className
        )}
        {...props}
    />
))
TabsContent.displayName = TabsPrimitive.Content.displayName

export { Tabs, TabsList, TabsTrigger, TabsContent }
