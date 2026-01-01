import { useState, useEffect, useCallback, useRef } from "react"
import { useNavigate, useSearchParams } from "react-router"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { HiOutlineEye, HiOutlineEyeOff, HiOutlineUser, HiOutlineLockClosed } from "react-icons/hi"
import ProxyGlobe from "./proxy-globe"
import { toast } from "sonner"
import { motion, useAnimation } from "framer-motion"
import logo from "@/assets/nasnet-logo.png"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import {
    Form,
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { loginSchema, type LoginFormData } from "@/lib/validations/login-schema"
import { useAuthStore } from "@/store/auth-store"
import { ApiError } from "@/lib/api"

// F: Animated number that counts up from 0 on mount
function AnimatedNumber({ target, decimals = 0, duration = 1500, suffix = "" }: {
    target: number; decimals?: number; duration?: number; suffix?: string
}) {
    const [value, setValue] = useState(0)
    const startTime = useRef(0)
    const animRef = useRef(0)

    useEffect(() => {
        startTime.current = performance.now()
        function tick(now: number) {
            const elapsed = now - startTime.current
            const progress = Math.min(elapsed / duration, 1)
            // Ease-out cubic
            const eased = 1 - Math.pow(1 - progress, 3)
            setValue(eased * target)
            if (progress < 1) animRef.current = requestAnimationFrame(tick)
        }
        animRef.current = requestAnimationFrame(tick)
        return () => cancelAnimationFrame(animRef.current)
    }, [target, duration])

    return <>{value.toFixed(decimals)}{suffix}</>
}

export default function LoginForm() {
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const callbackUrl = searchParams.get("callbackUrl") || "/dashboard"

    const [showPassword, setShowPassword] = useState(false)
    const [isLoading, setIsLoading] = useState(false)
    const [formError, setFormError] = useState<string | null>(null)
    const [lockoutSeconds, setLockoutSeconds] = useState(0)

    const login = useAuthStore((state) => state.login)
    const shakeControls = useAnimation()

    const form = useForm<LoginFormData>({
        resolver: zodResolver(loginSchema),
        defaultValues: {
            username: "",
            password: "",
            rememberMe: false,
        },
    })

    // Countdown timer for rate limit lockout
    useEffect(() => {
        if (lockoutSeconds <= 0) return
        const timer = setInterval(() => {
            setLockoutSeconds((prev) => {
                if (prev <= 1) {
                    setFormError(null)
                    return 0
                }
                return prev - 1
            })
        }, 1000)
        return () => clearInterval(timer)
    }, [lockoutSeconds])

    const triggerShake = useCallback(async () => {
        await shakeControls.start({
            x: [-8, 8, -4, 4, 0],
            transition: { duration: 0.4 },
        })
    }, [shakeControls])

    const isLocked = lockoutSeconds > 0

    async function onSubmit(data: LoginFormData) {
        setFormError(null)
        setIsLoading(true)

        try {
            const success = await login(data.username, data.password, data.rememberMe)
            if (success) {
                toast.success("Welcome back!")
                navigate(callbackUrl)
            } else {
                setFormError("Invalid username or password")
                triggerShake()
            }
        } catch (error) {
            if (error instanceof ApiError && error.status === 429) {
                // Parse seconds from error message like "too many attempts, try again in 300 seconds"
                const match = error.message.match(/(\d+)\s*seconds?/)
                const seconds = match ? parseInt(match[1], 10) : 300
                setLockoutSeconds(seconds)
                setFormError(`Too many failed attempts. Try again in ${seconds} seconds.`)
            } else {
                const message = error instanceof Error ? error.message : "Login failed"
                if (message.toLowerCase().includes("invalid") || message.toLowerCase().includes("credential")) {
                    setFormError(message)
                    triggerShake()
                } else {
                    toast.error(message)
                }
            }
        } finally {
            setIsLoading(false)
        }
    }

    return (
        <div
            className="min-h-screen flex bg-[#0a0a0a]"
            style={{ backgroundImage: "radial-gradient(circle, rgba(255,255,255,0.03) 1px, transparent 1px)", backgroundSize: "28px 28px" }}
        >
            {/* Left branding panel — desktop only */}
            <div className="hidden lg:flex lg:w-1/2 relative overflow-hidden flex-col items-center justify-center">
                {/* Radial glow behind globe */}
                <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[620px] h-[620px] rounded-full bg-sky-500/[0.07] blur-3xl pointer-events-none" />

                <motion.div
                    initial={{ opacity: 0, scale: 0.9 }}
                    animate={{ opacity: 1, scale: 1 }}
                    transition={{ duration: 0.8, delay: 0.2 }}
                    className="relative z-10 flex flex-col items-center gap-6"
                >
                    <ProxyGlobe />

                    <div className="text-center">
                        <div className="flex items-center justify-center gap-2.5 mb-2">
                            <img src={logo} alt="NasNet" className="w-9 h-9 rounded-full ring-1 ring-white/10 object-cover" />
                            <h1 className="text-3xl font-bold tracking-tight bg-gradient-to-r from-sky-400 via-cyan-300 to-blue-500 bg-clip-text text-transparent">
                                NasNet Panel
                            </h1>
                        </div>
                        <p className="text-white/50 text-sm">
                            Manage your xray proxy infrastructure
                        </p>
                    </div>

                    {/* Stat pills row */}
                    <motion.div
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.6, delay: 0.8 }}
                        className="flex items-center gap-3"
                    >
                        <span className="px-3 py-1.5 rounded-full bg-white/[0.06] border border-white/10 text-xs text-white/60 tabular-nums">
                            <AnimatedNumber target={18} duration={1200} /> Regions
                        </span>
                        <span className="px-3 py-1.5 rounded-full bg-white/[0.06] border border-white/10 text-xs text-white/60 tabular-nums">
                            <AnimatedNumber target={99.9} decimals={1} duration={1800} suffix="%" /> Uptime
                        </span>
                        <span className="px-3 py-1.5 rounded-full bg-white/[0.06] border border-white/10 text-xs text-white/60">
                            Low Latency
                        </span>
                    </motion.div>
                </motion.div>
            </div>

            {/* Right form panel */}
            <div className="flex-1 flex items-center justify-center p-6 sm:p-8 relative">
                {/* Subtle radial glow on the right side */}
                <div className="absolute top-1/2 left-0 -translate-y-1/2 w-[400px] h-[400px] rounded-full bg-sky-500/[0.03] blur-3xl pointer-events-none" />

                <motion.div
                    initial={{ opacity: 0, y: 24 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5 }}
                    className="w-full max-w-md relative z-10"
                >
                    {/* Mobile logo */}
                    <motion.div
                        initial={{ opacity: 0, scale: 0.9 }}
                        animate={{ opacity: 1, scale: 1 }}
                        transition={{ duration: 0.4 }}
                        className="lg:hidden flex flex-col items-center mb-8"
                    >
                        <div className="w-14 h-14 rounded-2xl overflow-hidden ring-1 ring-white/10 mb-4">
                            <img src={logo} alt="NasNet" className="w-full h-full object-cover" />
                        </div>
                        <h1 className="text-xl font-bold tracking-tight text-white">NasNet Panel</h1>
                    </motion.div>

                    {/* Glass card */}
                    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.03] backdrop-blur-sm p-8 shadow-2xl shadow-black/40">
                    <motion.div animate={shakeControls}>
                        <div className="space-y-2 mb-8">
                            <h2 className="text-2xl font-bold tracking-tight text-white">
                                Welcome back
                            </h2>
                            <p className="text-white/50 text-sm">
                                Sign in to continue to the dashboard
                            </p>
                        </div>

                        <Form {...form}>
                            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-5">
                                <motion.div
                                    initial={{ opacity: 0, y: 12 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.4, delay: 0.1 }}
                                >
                                    <FormField
                                        control={form.control}
                                        name="username"
                                        render={({ field }) => (
                                            <FormItem>
                                                <FormLabel className="text-white/70">Username</FormLabel>
                                                <FormControl>
                                                    <div className="relative">
                                                        <HiOutlineUser className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-white/40" />
                                                        <Input
                                                            placeholder="Enter your username"
                                                            autoComplete="username"
                                                            disabled={isLoading || isLocked}
                                                            className="pl-9 bg-white/[0.04] border-white/[0.08] text-white placeholder:text-white/30 focus-visible:border-sky-500/50 focus-visible:ring-sky-500/20"
                                                            {...field}
                                                        />
                                                    </div>
                                                </FormControl>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />
                                </motion.div>

                                <motion.div
                                    initial={{ opacity: 0, y: 12 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.4, delay: 0.15 }}
                                >
                                    <FormField
                                        control={form.control}
                                        name="password"
                                        render={({ field }) => (
                                            <FormItem>
                                                <FormLabel className="text-white/70">Password</FormLabel>
                                                <FormControl>
                                                    <div className="relative">
                                                        <HiOutlineLockClosed className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-white/40" />
                                                        <Input
                                                            type={showPassword ? "text" : "password"}
                                                            placeholder="Enter your password"
                                                            autoComplete="current-password"
                                                            disabled={isLoading || isLocked}
                                                            className="pl-9 pr-10 bg-white/[0.04] border-white/[0.08] text-white placeholder:text-white/30 focus-visible:border-sky-500/50 focus-visible:ring-sky-500/20"
                                                            {...field}
                                                        />
                                                        <button
                                                            type="button"
                                                            onClick={() => setShowPassword(!showPassword)}
                                                            className="absolute right-3 top-1/2 -translate-y-1/2 text-white/40 hover:text-white/70 transition-colors"
                                                            tabIndex={-1}
                                                        >
                                                            {showPassword ? (
                                                                <HiOutlineEyeOff className="w-4 h-4" />
                                                            ) : (
                                                                <HiOutlineEye className="w-4 h-4" />
                                                            )}
                                                        </button>
                                                    </div>
                                                </FormControl>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />
                                </motion.div>

                                <motion.div
                                    initial={{ opacity: 0, y: 12 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.4, delay: 0.2 }}
                                >
                                    <FormField
                                        control={form.control}
                                        name="rememberMe"
                                        render={({ field }) => (
                                            <FormItem className="flex flex-row items-center space-x-3 space-y-0">
                                                <FormControl>
                                                    <Checkbox
                                                        checked={field.value}
                                                        onCheckedChange={field.onChange}
                                                        disabled={isLoading || isLocked}
                                                    />
                                                </FormControl>
                                                <div className="space-y-1 leading-none">
                                                    <FormLabel className="cursor-pointer font-normal text-white/60">
                                                        Remember me
                                                    </FormLabel>
                                                </div>
                                            </FormItem>
                                        )}
                                    />
                                </motion.div>

                                {/* Inline error banner */}
                                {formError && (
                                    <motion.div
                                        initial={{ opacity: 0, height: 0 }}
                                        animate={{ opacity: 1, height: "auto" }}
                                        exit={{ opacity: 0, height: 0 }}
                                        className="rounded-lg bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive"
                                    >
                                        {formError}
                                        {isLocked && (
                                            <span className="ml-1 font-medium tabular-nums">
                                                ({lockoutSeconds}s)
                                            </span>
                                        )}
                                    </motion.div>
                                )}

                                <motion.div
                                    initial={{ opacity: 0, y: 12 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.4, delay: 0.25 }}
                                >
                                    <Button
                                        type="submit"
                                        className="w-full h-11 font-medium bg-sky-500 hover:bg-sky-400 text-white border-0"
                                        disabled={isLoading || isLocked}
                                    >
                                        {isLoading ? (
                                            <span className="flex items-center gap-2">
                                                <span className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin" />
                                                Signing in...
                                            </span>
                                        ) : isLocked ? (
                                            `Locked (${lockoutSeconds}s)`
                                        ) : (
                                            "Sign in"
                                        )}
                                    </Button>
                                </motion.div>
                            </form>
                        </Form>
                    </motion.div>
                    </div>
                </motion.div>
            </div>
        </div>
    )
}
