import { useState, useRef, useEffect } from "react"
import { motion, AnimatePresence } from "framer-motion"
import { Lock, Eye, EyeOff, Loader2, ShieldCheck } from "lucide-react"
import { PanelHeader } from "./panel-header"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { verifySubPassword } from "@/lib/sub-auth"

interface PasswordGateProps {
    uuid: string
    label: string
    onAuthenticated: () => void
}

export function PasswordGate({ uuid, label, onAuthenticated }: PasswordGateProps) {
    const [password, setPassword] = useState("")
    const [showPassword, setShowPassword] = useState(false)
    const [remember, setRemember] = useState(true)
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState("")
    const [shake, setShake] = useState(false)
    const inputRef = useRef<HTMLInputElement>(null)

    useEffect(() => {
        inputRef.current?.focus()
    }, [])

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault()
        if (!password.trim() || loading) return

        setLoading(true)
        setError("")

        const result = await verifySubPassword(uuid, password, remember)

        setLoading(false)

        if (result.success) {
            onAuthenticated()
        } else {
            setError(result.error === "invalid_password" ? "Wrong password" : (result.error || "Authentication failed"))
            setShake(true)
            setTimeout(() => setShake(false), 500)
            setPassword("")
            inputRef.current?.focus()
        }
    }

    return (
        <div className="min-h-screen bg-background">
            <PanelHeader />

            <main className="max-w-2xl md:max-w-3xl mx-auto px-4 md:px-6 py-4 md:py-6">
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ type: "spring", stiffness: 300, damping: 24 }}
                    className="flex items-center justify-center min-h-[60vh]"
                >
                    <Card className="w-full max-w-sm border-border/50 py-0 gap-0">
                        <CardContent className="p-6 sm:p-8">
                            <form onSubmit={handleSubmit} className="space-y-6">
                                {/* Icon & Title */}
                                <div className="flex flex-col items-center text-center space-y-3">
                                    <motion.div
                                        className="h-14 w-14 rounded-full bg-muted/50 flex items-center justify-center"
                                        initial={{ scale: 0.8, opacity: 0 }}
                                        animate={{ scale: 1, opacity: 1 }}
                                        transition={{ delay: 0.1, type: "spring", stiffness: 300, damping: 20 }}
                                    >
                                        <Lock className="h-6 w-6 text-muted-foreground" />
                                    </motion.div>
                                    <div>
                                        <h2 className="text-lg font-semibold tracking-tight">Enter Password</h2>
                                        <p className="text-sm text-muted-foreground mt-1 truncate max-w-[240px]">
                                            {label}
                                        </p>
                                    </div>
                                </div>

                                {/* Password Input */}
                                <motion.div
                                    animate={shake ? { x: [-8, 8, -6, 6, -3, 3, 0] } : {}}
                                    transition={{ duration: 0.4 }}
                                >
                                    <div className="relative">
                                        <Input
                                            ref={inputRef}
                                            type={showPassword ? "text" : "password"}
                                            placeholder="Password"
                                            value={password}
                                            onChange={(e) => {
                                                setPassword(e.target.value)
                                                if (error) setError("")
                                            }}
                                            className={`pr-10 h-11 ${error ? "border-red-500 focus-visible:ring-red-500/20" : ""}`}
                                            autoComplete="off"
                                            disabled={loading}
                                            aria-invalid={!!error}
                                            aria-describedby={error ? "sub-pw-error" : undefined}
                                        />
                                        <button
                                            type="button"
                                            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                                            onClick={() => setShowPassword(!showPassword)}
                                            tabIndex={-1}
                                        >
                                            {showPassword ? (
                                                <EyeOff className="h-4 w-4" />
                                            ) : (
                                                <Eye className="h-4 w-4" />
                                            )}
                                        </button>
                                    </div>

                                    <AnimatePresence>
                                        {error && (
                                            <motion.p
                                                initial={{ opacity: 0, height: 0 }}
                                                animate={{ opacity: 1, height: "auto" }}
                                                exit={{ opacity: 0, height: 0 }}
                                                id="sub-pw-error"
                                            role="alert"
                                            className="text-xs text-red-500 mt-1.5 pl-0.5"
                                            >
                                                {error}
                                            </motion.p>
                                        )}
                                    </AnimatePresence>
                                </motion.div>

                                {/* Remember Me */}
                                <div className="flex items-center gap-2">
                                    <Checkbox
                                        id="remember"
                                        checked={remember}
                                        onCheckedChange={(checked) => setRemember(checked === true)}
                                        disabled={loading}
                                    />
                                    <label
                                        htmlFor="remember"
                                        className="text-sm text-muted-foreground cursor-pointer select-none"
                                    >
                                        Remember me
                                    </label>
                                </div>

                                {/* Submit */}
                                <Button
                                    type="submit"
                                    className="w-full h-11 font-medium"
                                    disabled={!password.trim() || loading}
                                >
                                    {loading ? (
                                        <Loader2 className="h-4 w-4 animate-spin" />
                                    ) : (
                                        <>
                                            <ShieldCheck className="h-4 w-4 mr-2" />
                                            Unlock Panel
                                        </>
                                    )}
                                </Button>
                            </form>
                        </CardContent>
                    </Card>
                </motion.div>
            </main>
        </div>
    )
}
