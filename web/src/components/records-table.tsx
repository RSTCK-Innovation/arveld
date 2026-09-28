import { TableBody, TableCell, TableContainer, TableHead, TableRow } from '@mui/material';
import Table from '@mui/material/Table';
import type { ReactNode } from 'react';

export function RecordsTable({
  columns,
  rows,
  label,
}: {
  columns: string[];
  rows: { id: string; cells: ReactNode[] }[];
  label: string;
}) {
  return (
    <TableContainer tabIndex={0} role="region" aria-label={label}>
      <Table aria-label={label} sx={{ minWidth: 640 }}>
        <TableHead>
          <TableRow>
            {columns.map((c) => (
              <TableCell key={c}>{c}</TableCell>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.id}>
              {row.cells.map((cell, i) => (
                <TableCell key={columns[i]}>{cell}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}
