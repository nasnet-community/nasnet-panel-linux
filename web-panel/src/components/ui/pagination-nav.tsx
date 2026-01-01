import { Button } from "@/components/ui/button"
import { HiOutlineChevronLeft, HiOutlineChevronRight } from "react-icons/hi"
import { cn } from "@/lib/utils"

interface PaginationNavProps {
    page: number
    hasNextPage: boolean
    totalPages?: number
    onPageChange: (page: number) => void
    showingCount?: number
    className?: string
}

export function PaginationNav({
    page,
    hasNextPage,
    totalPages,
    onPageChange,
    showingCount,
    className,
}: PaginationNavProps) {
    const hasPrevPage = page > 1

    return (
        <div className={cn("flex flex-col-reverse sm:flex-row items-center sm:justify-between gap-4 sm:gap-0 pt-4 pb-8 sm:py-4", className)}>
            <p className="text-sm text-muted-foreground text-center sm:text-left w-full sm:w-auto">
                {totalPages
                    ? `Page ${page} of ${totalPages}`
                    : showingCount !== undefined
                        ? `Showing ${showingCount} items \u00b7 Page ${page}`
                        : `Page ${page}`}
            </p>
            <div className="flex items-center justify-between sm:justify-end gap-2 w-full sm:w-auto">
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onPageChange(page - 1)}
                    disabled={!hasPrevPage}
                >
                    <HiOutlineChevronLeft className="w-4 h-4 mr-1" />
                    Previous
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onPageChange(page + 1)}
                    disabled={!hasNextPage}
                >
                    Next
                    <HiOutlineChevronRight className="w-4 h-4 ml-1" />
                </Button>
            </div>
        </div>
    )
}
