/**
 * trigger/utils：浮层定位数学，1:1 复刻 Arco `trigger/utils.js`。
 * 包含 12 方位偏移、视口翻面、箭头偏移、transform-origin、滚动元素收集等。
 * 公式、边界判断、导出集合与 Arco 完全一致，仅补充 TS 类型。
 */
import { isArray } from "../_utils/is";
import { getDocumentSize } from "../_utils/dom";

export type TriggerPosition =
  | "top"
  | "tl"
  | "tr"
  | "bottom"
  | "bl"
  | "br"
  | "left"
  | "lt"
  | "lb"
  | "right"
  | "rt"
  | "rb";

export interface TriggerRect {
  top: number;
  bottom: number;
  left: number;
  right: number;
  scrollTop: number;
  scrollBottom: number;
  scrollLeft: number;
  scrollRight: number;
  width: number;
  height: number;
}

export interface Point {
  left: number;
  top: number;
}

type Translate = [number, number] | Partial<Record<TriggerPosition, [number, number]>>;

const getViewPortSize = (): { width: number; height: number } => {
  const { height, width } = getDocumentSize();
  return {
    width: Math.min(width, window.innerWidth),
    height: Math.min(height, window.innerHeight),
  };
};

export const getElementScrollRect = (
  element: HTMLElement,
  containerRect: DOMRect,
): TriggerRect => {
  const rect = element.getBoundingClientRect();
  return {
    top: rect.top,
    bottom: rect.bottom,
    left: rect.left,
    right: rect.right,
    scrollTop: rect.top - containerRect.top,
    scrollBottom: rect.bottom - containerRect.top,
    scrollLeft: rect.left - containerRect.left,
    scrollRight: rect.right - containerRect.left,
    width: element.offsetWidth ?? element.clientWidth,
    height: element.offsetHeight ?? element.clientHeight,
  };
};

const getBoundaryPosition = (position: TriggerPosition): "top" | "bottom" | "left" | "right" => {
  switch (position) {
    case "top":
    case "tl":
    case "tr":
      return "top";
    case "bottom":
    case "bl":
    case "br":
      return "bottom";
    case "left":
    case "lt":
    case "lb":
      return "left";
    case "right":
    case "rt":
    case "rb":
      return "right";
    default:
      return "top";
  }
};

const changePosition = (
  position: TriggerPosition,
  direction: "top" | "bottom" | "left" | "right",
): TriggerPosition => {
  switch (direction) {
    case "top":
      switch (position) {
        case "bottom":
          return "top";
        case "bl":
          return "tl";
        case "br":
          return "tr";
        default:
          return position;
      }
    case "bottom":
      switch (position) {
        case "top":
          return "bottom";
        case "tl":
          return "bl";
        case "tr":
          return "br";
        default:
          return position;
      }
    case "left":
      switch (position) {
        case "right":
          return "left";
        case "rt":
          return "lt";
        case "rb":
          return "lb";
        default:
          return position;
      }
    case "right":
      switch (position) {
        case "left":
          return "right";
        case "lt":
          return "rt";
        case "lb":
          return "rb";
        default:
          return position;
      }
    default:
      return position;
  }
};

const getFitPosition = (
  position: TriggerPosition,
  popupPosition: Point,
  {
    containerRect,
    triggerRect,
    popupRect,
    offset,
    translate,
  }: {
    containerRect: DOMRect;
    triggerRect: TriggerRect;
    popupRect: TriggerRect;
    offset: number;
    translate?: Translate;
  },
): { popupPosition: Point; position: TriggerPosition } => {
  const direction = getBoundaryPosition(position);
  const viewPortSize = getViewPortSize();
  const viewPortBoundary = {
    top: containerRect.top + popupPosition.top,
    bottom: viewPortSize.height - (containerRect.top + popupPosition.top + popupRect.height),
    left: containerRect.left + popupPosition.left,
    right: viewPortSize.width - (containerRect.left + popupPosition.left + popupRect.width),
  };
  let finalPosition = position;
  if (direction === "top" && viewPortBoundary.top < 0) {
    if (triggerRect.top > popupRect.height) {
      popupPosition.top = -containerRect.top;
    } else {
      const fitPosition = getPopupOffset("bottom", triggerRect, popupRect, { offset, translate });
      if (viewPortSize.height - (containerRect.top + fitPosition.top + popupRect.height) > 0) {
        finalPosition = changePosition(position, "bottom");
        popupPosition.top = fitPosition.top;
      }
    }
  }
  if (direction === "bottom" && viewPortBoundary.bottom < 0) {
    if (viewPortSize.height - triggerRect.bottom > popupRect.height) {
      popupPosition.top = -containerRect.top + (viewPortSize.height - popupRect.height);
    } else {
      const fitPosition = getPopupOffset("top", triggerRect, popupRect, { offset, translate });
      if (containerRect.top + fitPosition.top > 0) {
        finalPosition = changePosition(position, "top");
        popupPosition.top = fitPosition.top;
      }
    }
  }
  if (direction === "left" && viewPortBoundary.left < 0) {
    if (triggerRect.left > popupRect.width) {
      popupPosition.left = -containerRect.left;
    } else {
      const fitPosition = getPopupOffset("right", triggerRect, popupRect, { offset, translate });
      if (viewPortSize.width - (containerRect.left + fitPosition.left + popupRect.width) > 0) {
        finalPosition = changePosition(position, "right");
        popupPosition.left = fitPosition.left;
      }
    }
  }
  if (direction === "right" && viewPortBoundary.right < 0) {
    if (viewPortSize.width - triggerRect.right > popupRect.width) {
      popupPosition.left = -containerRect.left + (viewPortSize.width - popupRect.width);
    } else {
      const fitPosition = getPopupOffset("left", triggerRect, popupRect, { offset, translate });
      if (containerRect.left + fitPosition.left > 0) {
        finalPosition = changePosition(position, "left");
        popupPosition.left = fitPosition.left;
      }
    }
  }
  if (direction === "top" || direction === "bottom") {
    if (viewPortBoundary.left < 0) {
      popupPosition.left = -containerRect.left;
    } else if (viewPortBoundary.right < 0) {
      popupPosition.left = -containerRect.left + (viewPortSize.width - popupRect.width);
    }
  }
  if (direction === "left" || direction === "right") {
    if (viewPortBoundary.top < 0) {
      popupPosition.top = -containerRect.top;
    } else if (viewPortBoundary.bottom < 0) {
      popupPosition.top = -containerRect.top + (viewPortSize.height - popupRect.height);
    }
  }
  return {
    popupPosition,
    position: finalPosition,
  };
};

const getPopupOffset = (
  position: TriggerPosition,
  triggerRect: TriggerRect,
  popupRect: TriggerRect,
  { offset = 0, translate = [0, 0] as Translate }: { offset?: number; translate?: Translate } = {},
): Point => {
  const _translate = (isArray(translate) ? translate : (translate as any)[position]) ?? [0, 0];
  switch (position) {
    case "top":
      return {
        left: triggerRect.scrollLeft + Math.round(triggerRect.width / 2) - Math.round(popupRect.width / 2) + _translate[0],
        top: triggerRect.scrollTop - popupRect.height - offset + _translate[1],
      };
    case "tl":
      return {
        left: triggerRect.scrollLeft + _translate[0],
        top: triggerRect.scrollTop - popupRect.height - offset + _translate[1],
      };
    case "tr":
      return {
        left: triggerRect.scrollRight - popupRect.width + _translate[0],
        top: triggerRect.scrollTop - popupRect.height - offset + _translate[1],
      };
    case "bottom":
      return {
        left: triggerRect.scrollLeft + Math.round(triggerRect.width / 2) - Math.round(popupRect.width / 2) + _translate[0],
        top: triggerRect.scrollBottom + offset + _translate[1],
      };
    case "bl":
      return {
        left: triggerRect.scrollLeft + _translate[0],
        top: triggerRect.scrollBottom + offset + _translate[1],
      };
    case "br":
      return {
        left: triggerRect.scrollRight - popupRect.width + _translate[0],
        top: triggerRect.scrollBottom + offset + _translate[1],
      };
    case "left":
      return {
        left: triggerRect.scrollLeft - popupRect.width - offset + _translate[0],
        top: triggerRect.scrollTop + Math.round(triggerRect.height / 2) - Math.round(popupRect.height / 2) + _translate[1],
      };
    case "lt":
      return {
        left: triggerRect.scrollLeft - popupRect.width - offset + _translate[0],
        top: triggerRect.scrollTop + _translate[1],
      };
    case "lb":
      return {
        left: triggerRect.scrollLeft - popupRect.width - offset + _translate[0],
        top: triggerRect.scrollBottom - popupRect.height + _translate[1],
      };
    case "right":
      return {
        left: triggerRect.scrollRight + offset + _translate[0],
        top: triggerRect.scrollTop + Math.round(triggerRect.height / 2) - Math.round(popupRect.height / 2) + _translate[1],
      };
    case "rt":
      return {
        left: triggerRect.scrollRight + offset + _translate[0],
        top: triggerRect.scrollTop + _translate[1],
      };
    case "rb":
      return {
        left: triggerRect.scrollRight + offset + _translate[0],
        top: triggerRect.scrollBottom - popupRect.height + _translate[1],
      };
    default:
      return {
        left: 0,
        top: 0,
      };
  }
};

export const getTransformOrigin = (position: TriggerPosition): string => {
  let originX = "0";
  if (["top", "bottom"].includes(position)) {
    originX = "50%";
  } else if (["left", "lt", "lb", "tr", "br"].includes(position)) {
    originX = "100%";
  }
  let originY = "0";
  if (["left", "right"].includes(position)) {
    originY = "50%";
  } else if (["top", "tl", "tr", "lb", "rb"].includes(position)) {
    originY = "100%";
  }
  return `${originX} ${originY}`;
};

export const getPopupStyle = (
  position: TriggerPosition,
  containerRect: DOMRect,
  triggerRect: TriggerRect,
  popupRect: TriggerRect,
  {
    offset = 0,
    translate = [0, 0] as Translate,
    customStyle = {},
    autoFitPosition = false,
  }: {
    offset?: number;
    translate?: Translate;
    customStyle?: Record<string, any>;
    autoFitPosition?: boolean;
  } = {},
): { style: Record<string, any>; position: TriggerPosition } => {
  let finalPosition = position;
  let popupPosition = getPopupOffset(position, triggerRect, popupRect, { offset, translate });
  if (autoFitPosition) {
    const result = getFitPosition(position, popupPosition, {
      containerRect,
      popupRect,
      triggerRect,
      offset,
      translate,
    });
    popupPosition = result.popupPosition;
    finalPosition = result.position;
  }
  const style = {
    left: `${popupPosition.left}px`,
    top: `${popupPosition.top}px`,
    ...customStyle,
  };
  return {
    style,
    position: finalPosition,
  };
};

export const getArrowStyle = (
  position: TriggerPosition,
  triggerRect: TriggerRect,
  popupRect: TriggerRect,
  { customStyle = {} }: { customStyle?: Record<string, any> },
): Record<string, any> => {
  if (["top", "tl", "tr", "bottom", "bl", "br"].includes(position)) {
    let offsetLeft = Math.abs(triggerRect.scrollLeft + triggerRect.width / 2 - popupRect.scrollLeft);
    if (offsetLeft > popupRect.width - 8) {
      if (triggerRect.width > popupRect.width) {
        offsetLeft = popupRect.width / 2;
      } else {
        offsetLeft = popupRect.width - 8;
      }
    }
    if (["top", "tl", "tr"].includes(position)) {
      return {
        left: `${offsetLeft}px`,
        bottom: "0",
        transform: "translate(-50%,50%) rotate(45deg)",
        ...customStyle,
      };
    }
    return {
      left: `${offsetLeft}px`,
      top: "0",
      transform: "translate(-50%,-50%) rotate(45deg)",
      ...customStyle,
    };
  }
  let offsetTop = Math.abs(triggerRect.scrollTop + triggerRect.height / 2 - popupRect.scrollTop);
  if (offsetTop > popupRect.height - 8) {
    if (triggerRect.height > popupRect.height) {
      offsetTop = popupRect.height / 2;
    } else {
      offsetTop = popupRect.height - 8;
    }
  }
  if (["left", "lt", "lb"].includes(position)) {
    return {
      top: `${offsetTop}px`,
      right: "0",
      transform: "translate(50%,-50%) rotate(45deg)",
      ...customStyle,
    };
  }
  return {
    top: `${offsetTop}px`,
    left: "0",
    transform: "translate(-50%,-50%) rotate(45deg)",
    ...customStyle,
  };
};

export const isScrollElement = (element: HTMLElement): boolean => {
  return element.scrollHeight > element.offsetHeight || element.scrollWidth > element.offsetWidth;
};

export const getScrollElements = (container?: HTMLElement): HTMLElement[] => {
  const scrollElements: HTMLElement[] = [];
  let element: HTMLElement | undefined = container;
  while (element && element !== document.documentElement) {
    if (isScrollElement(element)) {
      scrollElements.push(element);
    }
    element = element.parentElement ?? undefined;
  }
  return scrollElements;
};
