import { FIELDS, type FieldId } from "./fields";

/** Something to tell the depositor about one field: a failed check of the
 *  form's own, or an error the API returned for it. */
export type FieldMessage = {
  field: FieldId;
  /**
   * Which item of a repeated field the message is about, by the id the
   * request gives it: an entity's label_entity_id, a crystal id ("1") or
   * crystal/dataset ids ("1/2"), a metric key, a run or file id. Null for a
   * field that occurs once.
   */
  item: string | null;
  /** Names the item in the list beside the submit button, for example "entity 1". */
  itemLabel?: string;
  message: string;
  /** False for advice the depositor may ignore. */
  blocking: boolean;
  /** A required value that is still empty. Listed beside the submit button
   *  only, so an untouched form is not covered in red. */
  missing?: boolean;
};

export function messagesFor(
  messages: readonly FieldMessage[],
  field: FieldId,
  item: string | null = null,
): FieldMessage[] {
  return messages.filter(
    (message) => message.field === field && message.item === item,
  );
}

/** The messages to print under the field itself. */
export function inlineMessages(
  messages: readonly FieldMessage[],
  field: FieldId,
  item: string | null = null,
): FieldMessage[] {
  return messagesFor(messages, field, item).filter((message) => !message.missing);
}

export function hasBlockingMessage(messages: readonly FieldMessage[]): boolean {
  return messages.some((message) => message.blocking);
}

/** One line per blocking message, for the list beside a disabled submit
 *  button: "Title: required", "Mutations (entity 1): …". */
export function blockerLines(messages: readonly FieldMessage[]): string[] {
  const lines = messages
    .filter((message) => message.blocking)
    .map((message) => {
      const label = FIELDS[message.field].label;
      const where = message.itemLabel ? `${label} (${message.itemLabel})` : label;
      return `${where}: ${message.message}`;
    });
  return Array.from(new Set(lines));
}

export function required(
  field: FieldId,
  item: string | null = null,
  itemLabel?: string,
): FieldMessage {
  return { field, item, itemLabel, message: "required", blocking: true, missing: true };
}

export function invalid(
  field: FieldId,
  message: string,
  item: string | null = null,
  itemLabel?: string,
): FieldMessage {
  return { field, item, itemLabel, message, blocking: true };
}

export function advice(
  field: FieldId,
  message: string,
  item: string | null = null,
): FieldMessage {
  return { field, item, message, blocking: false };
}
