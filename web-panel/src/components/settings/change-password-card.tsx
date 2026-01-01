import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { HiOutlineKey, HiOutlineEye, HiOutlineEyeOff } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { useChangePassword } from "@/lib/queries"
import {
    changePasswordSchema,
    type ChangePasswordFormData,
} from "@/lib/validations/change-password-schema"

export function ChangePasswordForm() {
    const changePassword = useChangePassword()
    const [showCurrent, setShowCurrent] = useState(false)
    const [showNew, setShowNew] = useState(false)
    const [showConfirm, setShowConfirm] = useState(false)

    const {
        register,
        handleSubmit,
        reset,
        formState: { errors },
    } = useForm<ChangePasswordFormData>({
        resolver: zodResolver(changePasswordSchema),
        defaultValues: {
            current_password: "",
            new_password: "",
            confirm_password: "",
        },
    })

    const onSubmit = (data: ChangePasswordFormData) => {
        changePassword.mutate(data, {
            onSuccess: () => {
                reset()
                setShowCurrent(false)
                setShowNew(false)
                setShowConfirm(false)
            },
        })
    }

    return (
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 max-w-md">
            <div className="space-y-2">
                <Label htmlFor="current_password">Current Password</Label>
                <div className="relative">
                    <Input
                        id="current_password"
                        type={showCurrent ? "text" : "password"}
                        {...register("current_password")}
                        className="pr-10"
                    />
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
                        onClick={() => setShowCurrent(!showCurrent)}
                        aria-label={showCurrent ? "Hide current password" : "Show current password"}
                    >
                        {showCurrent ? (
                            <HiOutlineEyeOff className="w-4 h-4" />
                        ) : (
                            <HiOutlineEye className="w-4 h-4" />
                        )}
                    </Button>
                </div>
                {errors.current_password && (
                    <p className="text-sm text-destructive">{errors.current_password.message}</p>
                )}
            </div>

            <div className="space-y-2">
                <Label htmlFor="new_password">New Password</Label>
                <div className="relative">
                    <Input
                        id="new_password"
                        type={showNew ? "text" : "password"}
                        {...register("new_password")}
                        className="pr-10"
                    />
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
                        onClick={() => setShowNew(!showNew)}
                        aria-label={showNew ? "Hide new password" : "Show new password"}
                    >
                        {showNew ? (
                            <HiOutlineEyeOff className="w-4 h-4" />
                        ) : (
                            <HiOutlineEye className="w-4 h-4" />
                        )}
                    </Button>
                </div>
                {errors.new_password && (
                    <p className="text-sm text-destructive">{errors.new_password.message}</p>
                )}
            </div>

            <div className="space-y-2">
                <Label htmlFor="confirm_password">Confirm New Password</Label>
                <div className="relative">
                    <Input
                        id="confirm_password"
                        type={showConfirm ? "text" : "password"}
                        {...register("confirm_password")}
                        className="pr-10"
                    />
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
                        onClick={() => setShowConfirm(!showConfirm)}
                        aria-label={showConfirm ? "Hide confirm password" : "Show confirm password"}
                    >
                        {showConfirm ? (
                            <HiOutlineEyeOff className="w-4 h-4" />
                        ) : (
                            <HiOutlineEye className="w-4 h-4" />
                        )}
                    </Button>
                </div>
                {errors.confirm_password && (
                    <p className="text-sm text-destructive">{errors.confirm_password.message}</p>
                )}
            </div>

            <Button type="submit" disabled={changePassword.isPending}>
                {changePassword.isPending && (
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                )}
                Change Password
            </Button>
        </form>
    )
}

export function ChangePasswordCard() {
    return (
        <Card>
            <CardHeader>
                <div className="flex items-center gap-3">
                    <div className="p-2 rounded-lg bg-primary/10">
                        <HiOutlineKey className="w-5 h-5 text-primary" />
                    </div>
                    <div>
                        <CardTitle>Change Password</CardTitle>
                        <CardDescription>
                            Update your admin panel login password
                        </CardDescription>
                    </div>
                </div>
            </CardHeader>
            <CardContent>
                <ChangePasswordForm />
            </CardContent>
        </Card>
    )
}
