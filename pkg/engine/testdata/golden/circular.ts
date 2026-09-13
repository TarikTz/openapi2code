export interface A {
  b?: B;
}

export interface B {
  a?: A;
}

export interface TreeNode {
  children?: TreeNode[];
  value: string;
}

