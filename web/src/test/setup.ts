import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => cleanup());

// jsdom does not implement scrolling; navigate() calls it.
window.scrollTo = () => {};
