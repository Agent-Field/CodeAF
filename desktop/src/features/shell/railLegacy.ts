/**
 * Whether the Design system rail row is drawn.
 * Shell 3a does not draw that page. A development build keeps one row so the specimens stay reachable;
 * a production build passes false and the row is absent even when a link was supplied.
 */
export function designSystemRow<T>(dev: boolean, link: T | undefined): T | undefined {
  return dev && link !== undefined ? link : undefined;
}
