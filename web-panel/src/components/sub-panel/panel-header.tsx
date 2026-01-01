import { useTheme } from "@/components/providers/theme-provider"
import { Moon, Sun, Shield } from "lucide-react"
import { Button } from "@/components/ui/button"
import { motion, useScroll, useMotionValueEvent } from "framer-motion"
import { useState } from "react"

export function PanelHeader() {
    const { theme, setTheme } = useTheme()
    const [scrolled, setScrolled] = useState(false)
    const { scrollY } = useScroll()

    useMotionValueEvent(scrollY, "change", (latest) => {
        setScrolled(latest > 10)
    })

    return (
        <motion.header
            initial={{ y: -20, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            transition={{ type: "spring", stiffness: 300, damping: 24 }}
            className="bg-background/40 backdrop-blur-md sticky top-0 z-10 transition-[border-color,box-shadow] duration-300"
            style={{
                borderBottom: "1px solid",
                borderColor: scrolled ? "hsl(var(--border) / 0.5)" : "transparent",
                boxShadow: scrolled ? "0 1px 3px 0 rgb(0 0 0 / 0.05)" : "none",
            }}
        >
            <div className="max-w-2xl md:max-w-3xl mx-auto px-4 md:px-6 h-14 flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Shield className="h-5 w-5 md:h-6 md:w-6 text-primary" />
                    <span className="font-semibold text-sm md:text-base tracking-tight">Subscription</span>
                </div>
                <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 relative"
                    onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
                >
                    <motion.div
                        key={theme}
                        initial={false}
                        animate={{ rotate: 0, scale: 1, opacity: 1 }}
                        exit={{ rotate: 180, scale: 0, opacity: 0 }}
                        transition={{ type: "spring", stiffness: 300, damping: 20 }}
                    >
                        {theme === "dark" ? (
                            <Moon className="h-4 w-4" />
                        ) : (
                            <Sun className="h-4 w-4" />
                        )}
                    </motion.div>
                    <span className="sr-only">Toggle theme</span>
                </Button>
            </div>
        </motion.header>
    )
}
