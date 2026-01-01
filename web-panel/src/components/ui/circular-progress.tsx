import * as React from "react"
import { motion, useMotionValue, useSpring, useTransform } from "framer-motion"
import { cn } from "@/lib/utils"

interface CircularProgressProps {
    value: number // 0-100
    size?: number // default 120
    strokeWidth?: number // default 8
    color?: "emerald" | "amber" | "red" | "blue" | "purple"
    showValue?: boolean
    label?: string
    className?: string
    children?: React.ReactNode
}

const colorMap = {
    emerald: "stroke-emerald-500",
    amber: "stroke-amber-500",
    red: "stroke-red-500",
    blue: "stroke-blue-500",
    purple: "stroke-purple-500",
}

const bgColorMap = {
    emerald: "stroke-emerald-500/20",
    amber: "stroke-amber-500/20",
    red: "stroke-red-500/20",
    blue: "stroke-blue-500/20",
    purple: "stroke-purple-500/20",
}

export function CircularProgress({
    value,
    size = 120,
    strokeWidth = 8,
    color = "emerald",
    showValue = true,
    label,
    className,
    children,
}: CircularProgressProps) {
    const radius = (size - strokeWidth) / 2
    const circumference = radius * 2 * Math.PI

    const motionValue = useMotionValue(0)
    const spring = useSpring(motionValue, { stiffness: 26, damping: 12, mass: 1.2 })
    const strokeDashoffset = useTransform(
        spring,
        (v: number) => circumference - (Math.min(v, 100) / 100) * circumference
    )

    React.useEffect(() => {
        motionValue.set(value)
    }, [value, motionValue])

    return (
        <div className={cn("relative inline-flex items-center justify-center shrink-0", className)}
            style={{ width: size, height: size }}
        >
            <svg
                viewBox={`0 0 ${size} ${size}`}
                className="w-full h-full transform -rotate-90"
            >
                {/* Background circle */}
                <circle
                    cx={size / 2}
                    cy={size / 2}
                    r={radius}
                    fill="none"
                    strokeWidth={strokeWidth}
                    className={bgColorMap[color]}
                />
                {/* Progress circle — physics-based spring fill */}
                <motion.circle
                    cx={size / 2}
                    cy={size / 2}
                    r={radius}
                    fill="none"
                    strokeWidth={strokeWidth}
                    strokeLinecap="round"
                    className={colorMap[color]}
                    style={{
                        strokeDasharray: circumference,
                        strokeDashoffset,
                    }}
                />
            </svg>
            {/* Center content */}
            <div className="absolute inset-0 flex flex-col items-center justify-center text-center">
                {children ? children : (
                    <>
                        {showValue && (
                            <span className="text-2xl font-bold tracking-tight">
                                {Math.round(value)}%
                            </span>
                        )}
                        {label && (
                            <span className="text-[10px] uppercase font-medium text-muted-foreground tracking-wider mt-0.5">
                                {label}
                            </span>
                        )}
                    </>
                )}
            </div>
        </div>
    )
}
