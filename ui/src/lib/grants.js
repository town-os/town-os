/**
 * The grants an account can hold, mirroring account.AllGrants on the server.
 *
 * Adding a grant is one entry here: both the create and edit forms render their
 * checkboxes from this list, so a new capability needs no new markup.
 */
export const GRANT_WIREGUARD = 'wireguard'
export const GRANT_GFEH = 'gfeh'

export const GRANTS = [
  { name: GRANT_WIREGUARD, labelKey: 'users.grant_wireguard' },
  { name: GRANT_GFEH, labelKey: 'users.grant_gfeh' },
]
