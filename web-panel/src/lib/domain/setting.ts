export interface Setting {
    key: string;
    value: string;
    type: string; // "string" | "int" | "bool" | "json"
    category: string;
    description: string;
    label: string;
    sensitive?: boolean;
    requires_restart?: boolean;
}

export interface SettingsGrouped {
    [category: string]: Setting[];
}
