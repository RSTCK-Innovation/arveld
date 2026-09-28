import Button from '@mui/material/Button';
import MuiLink from '@mui/material/Link';
import ListItemButton from '@mui/material/ListItemButton';
import { createLink } from '@tanstack/react-router';
export const ButtonLink = createLink(Button);
export const NavLink = createLink(ListItemButton);

export const AppLink = createLink(MuiLink);
