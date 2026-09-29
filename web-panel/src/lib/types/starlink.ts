export interface StarlinkStatus {
    available: boolean
    downlink_throughput_bps: number
    uplink_throughput_bps: number
    pop_ping_latency_ms: number
    pop_ping_drop_rate: number
    eth_speed_mbps: number
    obstruction_fraction: number
    currently_obstructed: boolean
    avg_prolonged_obstruction_duration_s: number
    avg_prolonged_obstruction_interval_s: number
    alert_thermal_throttle: boolean
    alert_thermal_shutdown: boolean
    alert_is_heating: boolean
    alert_slow_ethernet: boolean
    alert_power_save_idle: boolean
    alert_motors_stuck: boolean
    alert_no_ethernet_link: boolean
    alert_unexpected_location: boolean
    alert_roaming: boolean
    alert_mast_not_near_vertical: boolean
    alert_install_pending: boolean
    hardware_version: string
    software_version: string
    country_code: string
    uptime_s: number
    boot_count: number
    gps_valid: boolean
    gps_sats: number
    tilt_angle_deg: number
    boresight_azimuth_deg: number
    boresight_elevation_deg: number
    attitude_uncertainty_deg: number
    attitude_estimation_state: string
    desired_boresight_azimuth_deg: number
    desired_boresight_elevation_deg: number
    actuator_state: string
    has_actuators: string
    software_update_state: string
    software_update_progress: number
    outage_cause: string
    outage_duration_ns: number
    disablement_code: string
    mobility_class: string
    class_of_service: string
    cell_id: number
    satellite_id: number
    gateway_id: number
    on_backup_beam: boolean
    latitude: number
    longitude: number
    altitude: number
    is_snr_above_noise_floor: boolean
    is_snr_persistently_low: boolean
    // Optional: legacy agents do not send these.
    passthrough?: boolean
    signal_quality?: number
    ready_states?: Record<string, boolean>
    dl_bandwidth_restricted_reason?: string
    ul_bandwidth_restricted_reason?: string
    gps_inhibited?: boolean
    pnt_filter_state?: string
    stow_requested?: boolean
    update_requires_reboot?: boolean
    swupdate_reboot_ready?: boolean
    reboot_reason?: string
    is_moving_fast?: boolean
    init_durations?: Record<string, number>
    context_locked?: boolean
    location_locked?: boolean
    alert_lower_signal_than_predicted?: boolean
    alert_obstruction_map_reset?: boolean
    alert_dish_water_detected?: boolean
    alert_power_supply_thermal_throttle?: boolean
    alert_slow_ethernet_100?: boolean
}

export interface StarlinkObstructionMap {
    num_rows: number
    num_cols: number
    snr: (number | null)[]
    max_theta_deg: number
    reference_frame: string
}

export interface StarlinkDataPoint {
    id: number
    node_id: number
    downlink_throughput_bps: number
    uplink_throughput_bps: number
    pop_ping_latency_ms: number
    pop_ping_drop_rate: number
    obstruction_fraction: number
    currently_obstructed: boolean
    tilt_angle_deg: number
    boresight_azimuth_deg: number
    boresight_elevation_deg: number
    gps_valid: boolean
    alert_flags: number
    created_at: string
}


export interface StarlinkOutage {
    cause: string
    start_at: string
    duration_ms: number
    did_switch: boolean
}

export interface StarlinkEvent {
    id?: number
    severity: string
    reason: string
    start_at: string
    duration_ms: number
}

/** The dish's own rolling 15-minute buffer, bucketed by step_s. Point i is at end_at - (len-1-i)*step_s. */
export interface StarlinkLive {
    end_at: string
    step_s: number
    pop_ping_latency_ms: (number | null)[]
    pop_ping_drop_rate: (number | null)[]
    downlink_throughput_bps: (number | null)[]
    uplink_throughput_bps: (number | null)[]
    power_w: (number | null)[]
    outages: StarlinkOutage[]
    events: StarlinkEvent[]
}

export interface StarlinkDishConfig {
    snow_melt_mode: string
    location_request_mode: string
    level_dish_mode: string
    power_save_mode: boolean
    power_save_start_minutes: number
    power_save_duration_minutes: number
    swupdate_reboot_hour: number
    swupdate_three_day_deferral: boolean
    asset_class: number
}

export interface StarlinkDevice {
    id: string
    hardware_version: string
    software_version: string
    manufactured_version: string
    build_id: string
    country_code: string
    utc_offset_s: number
    boot_count: number
    hardware_self_test: "PASSED" | "FAILED" | "NO_RESULT"
    hardware_self_test_codes: number[] | null
    stowed: boolean
    config?: StarlinkDishConfig
}

export type StarlinkAction =
    | "reboot" | "stow" | "unstow" | "clear-map" | "self-test"
    | "gps-inhibit" | "gps-restore" | "factory-reset"

export interface StarlinkActionResult {
    action: StarlinkAction
    message: string
    self_test?: { passed: boolean; report: string }
}
