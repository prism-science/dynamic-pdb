/** The query parameter the Delete entry action adds when it lands on the
 *  entries list, so the list can say which entry was deleted. */
export const ENTRY_DELETED_PARAM = "deleted";

export function entryDeletedNotice(entryId: string): string {
  return `Entry ${entryId} was deleted.`;
}
