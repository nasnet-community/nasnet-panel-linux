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

export interface OutboundTestResult {
    success: boolean
    latency_ms: number
    status_code: number
    ip: string
    country: string
    error: string
    message: string
}
