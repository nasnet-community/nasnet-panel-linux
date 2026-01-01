import React, { useState } from 'react';
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { VLESSSettings } from "@/lib/types";
import { Info, AlertTriangle, KeyRound, Shield, Loader2 } from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { generateVLESSKeys } from "@/lib/admin-api";
import { cn } from "@/lib/utils";
import { FallbacksForm } from "./fallbacks-form";

interface VLESSKeyPair {
    label: string;
    decryption: string;
    encryption: string;
}

interface VLESSFormProps {
    data: VLESSSettings;
    onChange: (data: VLESSSettings) => void;
    mode: 'inbound' | 'outbound';
    network?: string;
    security?: string;
}

export function VLESSForm({ data, onChange, mode, network, security }: VLESSFormProps) {
    const [generatedKeys, setGeneratedKeys] = useState<VLESSKeyPair[]>([]);
    const [selectedKeyIndex, setSelectedKeyIndex] = useState<number>(0);
    const [generating, setGenerating] = useState(false);

    const handleFlowChange = (value: string) => {
        onChange({ ...data, flow: value === "none" ? "" : value });
    }

    const handleGenerateKeys = async () => {
        setGenerating(true);
        try {
            const res = await generateVLESSKeys();
            if (res.success && res.data && res.data.length > 0) {
                setGeneratedKeys(res.data);
                setSelectedKeyIndex(0);
                // Apply first key automatically
                const key = res.data[0];
                onChange({
                    ...data,
                    decryption: key.decryption,
                    encryption: key.encryption,
                });
                toast.success("Keys generated successfully");
            } else {
                toast.error("Failed to generate keys");
            }
        } catch {
            toast.error("Error generating keys");
        } finally {
            setGenerating(false);
        }
    };

    const handleKeyTypeSelect = (index: number) => {
        setSelectedKeyIndex(index);
        const key = generatedKeys[index];
        onChange({
            ...data,
            decryption: key.decryption,
            encryption: key.encryption,
        });
    };

    // XTLS Vision requires TCP/raw + TLS/Reality OR XHTTP
    const canUseVision = ((network === 'tcp' || network === 'raw') && (security === 'tls' || security === 'reality'))
        || (network === 'xhttp');

    return (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {mode === 'outbound' && (
                <div className="space-y-2">
                    <Label htmlFor="vless-uuid">UUID</Label>
                    <Input
                        id="vless-uuid"
                        value={data.uuid || ""}
                        onChange={(e) => onChange({ ...data, uuid: e.target.value })}
                        placeholder="UUID"
                    />
                </div>
            )}

            <div className="space-y-2">
                <div className="flex items-center gap-2">
                    <Label>Flow</Label>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger>
                                <Info className="h-4 w-4 text-muted-foreground" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {mode === 'inbound'
                                    ? "Default flow setting for users on this inbound. If a user has no specific flow set, this value will be used."
                                    : "Flow setting for this outbound connection (e.g., to a remote server)."
                                }
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                    {!canUseVision && (data.flow === 'xtls-rprx-vision' || data.flow === 'xtls-rprx-vision-udp443') && (
                        <span className="flex items-center gap-1 text-[10px] text-amber-500 font-medium bg-amber-500/10 px-1.5 py-0.5 rounded">
                            <AlertTriangle className="w-3 h-3" />
                            Incompatible Network/Security
                        </span>
                    )}
                </div>
                <Select
                    value={data.flow || "none"}
                    onValueChange={handleFlowChange}
                >
                    <SelectTrigger className="w-full">
                        <SelectValue placeholder="Select flow..." />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="none">None</SelectItem>
                        <SelectItem value="xtls-rprx-vision" disabled={!canUseVision}>
                            <div className="flex items-center justify-between w-full">
                                <span>xtls-rprx-vision</span>
                                {!canUseVision && <span className="text-xs text-muted-foreground ml-2">(Requires TCP + TLS/Reality or XHTTP)</span>}
                            </div>
                        </SelectItem>
                        {/* vision-udp443 is outbound-only; inbounds reject it */}
                        {mode === 'outbound' && (
                            <SelectItem value="xtls-rprx-vision-udp443" disabled={!canUseVision}>
                                <div className="flex items-center justify-between w-full">
                                    <span>xtls-rprx-vision-udp443</span>
                                    {!canUseVision && <span className="text-xs text-muted-foreground ml-2">(Requires TCP + TLS/Reality or XHTTP)</span>}
                                </div>
                            </SelectItem>
                        )}
                    </SelectContent>
                </Select>
            </div>

            <div className="space-y-4 col-span-1 md:col-span-2 border rounded-md p-4 bg-muted/20">
                <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">Encryption & Decryption (Authentication)</h4>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={generating}
                        onClick={handleGenerateKeys}
                    >
                        {generating ? (
                            <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                        ) : (
                            <KeyRound className="h-3.5 w-3.5 mr-1.5" />
                        )}
                        Generate Keys
                    </Button>
                </div>

                {/* Key type selector - shown after generation */}
                {generatedKeys.length > 1 && (
                    <div className="space-y-2">
                        <Label className="text-xs text-muted-foreground">Authentication Type</Label>
                        <div className="grid grid-cols-2 gap-2">
                            {generatedKeys.map((key, index) => {
                                const labelLower = key.label.toLowerCase();
                                const isPostQuantum = labelLower.includes("post-quantum") && !labelLower.includes("not post-quantum");
                                const isSelected = selectedKeyIndex === index;
                                return (
                                    <button
                                        key={index}
                                        type="button"
                                        onClick={() => handleKeyTypeSelect(index)}
                                        className={cn(
                                            "relative flex flex-col items-start gap-1 rounded-lg border p-3 text-left text-sm transition-all",
                                            isSelected
                                                ? "border-primary bg-primary/5 ring-1 ring-primary/30"
                                                : "border-border hover:border-muted-foreground/30 hover:bg-accent/50"
                                        )}
                                    >
                                        <div className="flex items-center gap-2">
                                            <Shield className={cn(
                                                "h-4 w-4",
                                                isSelected ? "text-primary" : "text-muted-foreground"
                                            )} />
                                            <span className="font-medium">
                                                {key.label.split(",")[0].trim()}
                                            </span>
                                        </div>
                                        {isPostQuantum ? (
                                            <span className="text-[10px] font-medium text-emerald-500">Post-Quantum Safe</span>
                                        ) : (
                                            <span className="text-[10px] text-muted-foreground">Classic</span>
                                        )}
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                )}

                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-2">
                        <Label>Decryption</Label>
                        <Input
                            value={data.decryption || ""}
                            onChange={(e) => onChange({ ...data, decryption: e.target.value })}
                            placeholder="none"
                        />
                        <p className="text-[10px] text-muted-foreground">Server side (Private Key/ID)</p>
                    </div>
                    <div className="space-y-2">
                        <Label>Encryption</Label>
                        <Input
                            value={data.encryption || ""}
                            onChange={(e) => onChange({ ...data, encryption: e.target.value })}
                            placeholder="none"
                        />
                        <p className="text-[10px] text-muted-foreground">Client side (Public Key/ID)</p>
                    </div>
                </div>
            </div>

            {mode === 'inbound' && (
                <div className="col-span-1 md:col-span-2">
                    <FallbacksForm
                        fallbacks={data.fallbacks || []}
                        onChange={(fallbacks) => onChange({ ...data, fallbacks })}
                    />
                </div>
            )}
        </div>
    );
}
