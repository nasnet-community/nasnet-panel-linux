import type { Message } from './lang'

// English is the source: fa.ts must have every key written here.
export const en = {
  // shared
  'common.close': 'Close',
  'common.cancel': 'Cancel',
  'common.copy': 'Copy',
  'common.copied': 'Copied',
  'common.dismiss': 'Dismiss',

  // sizes, rates and times
  'unit.B': '{value} B',
  'unit.KB': '{value} KB',
  'unit.MB': '{value} MB',
  'unit.GB': '{value} GB',
  'unit.TB': '{value} TB',
  'unit.Bps': '{value} B/s',
  'unit.KBps': '{value} KB/s',
  'unit.MBps': '{value} MB/s',
  'unit.GBps': '{value} GB/s',
  'unit.TBps': '{value} TB/s',
  'duration.s': '{s}s',
  'duration.m': '{m}m',
  'duration.hm': '{h}h {m}m',
  'duration.dh': '{d}d {h}h',
  'time.justNow': 'just now',
  'time.secondsAgo': '{s} s ago',
  'time.today': 'today',

  // the page
  'app.subtitle': 'Entry nodes whose traffic leaves through this router',
  'app.themeToLight': 'Switch to light theme',
  'app.themeToDark': 'Switch to dark theme',
  'tabs.sections': 'Sections',
  'tabs.nodes': 'Nodes',
  'tabs.subscriptions': 'Subscriptions',
  'tabs.status': 'Status',
  'tabs.settings': 'Settings',
  'tabs.node': 'Node sections',
  'soon': 'Not available yet.',
  'node.all': 'All nodes',

  // the entry card
  'entry.on': 'Entry is on',
  'entry.off': 'Entry is off',
  'entry.checked': 'Checked {ago}',
  'entry.notChecked': 'Not checked yet',
  'entry.stale': 'Checked {ago}, may be out of date',
  'entry.domestic': 'Domestic link',
  'entry.vpn': 'VPN',
  'entry.bridge': 'Bridge',
  'entry.online': 'online',
  'entry.offline': 'offline',
  'entry.missing': 'missing',
  'entry.running': 'running',
  'entry.waiting': 'waiting',
  'entry.stopped': 'stopped',
  'vpn.wireguard': 'WireGuard',
  'vpn.l2tp': 'L2TP',
  'vpn.openvpn': 'OpenVPN',

  // what needs attention
  'attn.label': 'Needs attention',
  'attn.openSetup': 'Open setup',
  'attn.noDomestic.title': 'This router has no Domestic link',
  'attn.noDomestic.detail':
    "Subscriptions can't reach their exit until it's set up. You can still add and update nodes. Add a Domestic link in NasNet Panel's setup.",
  'attn.noVpn.title': 'This router has no VPN',
  'attn.noVpn.detail': "Traffic from subscriptions leaves through the VPN. Add one in NasNet Panel's setup.",
  'attn.domesticDown.title': 'The Domestic link is offline',
  'attn.domesticDown.detail': "Subscriptions can't reach their exit until it is back. Check the link in NasNet Panel.",
  'attn.vpnDown.title': 'The VPN is offline',
  'attn.vpnDown.detail': 'Traffic from subscriptions has no exit until it is back. Check the VPN in NasNet Panel.',
  'attn.routerOS.title': 'RouterOS {version} is too old',
  'attn.routerOS.detail': 'Remote Nodes needs RouterOS {min} or newer. Update RouterOS, then reload this page.',

  // the API's error codes
  'error.forbidden': 'This request was refused. Reload the page and try again.',
  'error.not_found': 'It no longer exists. Reload the page.',
  'error.node_limit': 'This router has reached its limit of nodes. Remove one to add another.',
  'error.subscription_limit': 'This router has reached its limit of subscriptions. Remove one to add another.',
  'error.name_taken': 'That name is already used.',
  'error.entry_off': "The entry is off, so this isn't available yet.",
  'error.node_unreachable': "The router can't reach this node. Check that it is on and its address is on the domestic list.",
  'error.needs_login': "The router can't reach this node, so it needs an SSH login.",
  'error.version_mismatch': 'This node runs a different Xray than this router uses. Update Xray first.',
  'error.agent_too_old': "This node's agent is too old. Update the agent first.",
  'error.host_key_changed': "The node's SSH key changed. Check it before you go on.",
  'error.busy': 'Another job is already running on this node.',
  'error.backup_bad_passphrase': "That passphrase doesn't open this backup.",
  'error.backup_bad_file': "This file isn't a Remote Nodes backup.",
} satisfies Record<string, Message>

export type MsgKey = keyof typeof en
