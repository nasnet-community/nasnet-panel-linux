import { useState, useCallback } from "react"
import { URL_KEYS } from "./settings-constants"

interface UseSettingValidationProps {
    type: string
    key: string
    label?: string
}

interface UseSettingValidationReturn {
    error: string | null
    onBlur: (value: string) => void
    onChange: (value: string) => void
    clearError: () => void
}

export function useSettingValidation({ type, key, label }: UseSettingValidationProps): UseSettingValidationReturn {
    const [error, setError] = useState<string | null>(null)

    const validate = useCallback((value: string, isBlur: boolean): string | null => {
        if (type === "int") {
            if (value !== "" && !/^-?\d+$/.test(value)) return "Must be a whole number"
            if (value !== "" && Number(value) < 0) return "Must not be negative"
        }

        if (type === "string" && isBlur) {
            const isOptional = label?.toLowerCase().includes("optional")
            if (!isOptional && value.trim() === "") return "This field is required"
        }

        if (URL_KEYS.includes(key) && isBlur && value.trim() !== "") {
            try {
                new URL(value)
            } catch {
                return "Must be a valid URL"
            }
        }

        return null
    }, [type, key, label])

    const onBlur = useCallback((value: string) => {
        setError(validate(value, true))
    }, [validate])

    const onChange = useCallback((value: string) => {
        // Clear error immediately when user starts correcting
        if (error) {
            const newError = validate(value, false)
            if (!newError) setError(null)
        }
    }, [error, validate])

    const clearError = useCallback(() => setError(null), [])

    return { error, onBlur, onChange, clearError }
}
