import { createLink } from '@tanstack/react-router'
import type { ComponentProps } from 'react'
import { ArrowLeftIcon } from '../icons/icons'
import './back-link.css'

function BackAnchor({ className, children, ...props }: ComponentProps<'a'>) {
  return (
    <a {...props} className={className ? `back-link ${className}` : 'back-link'}>
      <ArrowLeftIcon size={14} />
      {children}
    </a>
  )
}

// A "back to the parent page" link above a page title: arrow and label in secondary text, no underline.
export const BackLink = createLink(BackAnchor)
